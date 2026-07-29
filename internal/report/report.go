// Package report reads meter and audit JSONL files and produces aggregate
// build statistics for RECEIPTS.md output.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/benchratio"
	"github.com/amargautam/pakka/internal/calibrate"
	"github.com/amargautam/pakka/internal/data"
	"github.com/amargautam/pakka/internal/meter"
	"github.com/amargautam/pakka/internal/pricing"
	"github.com/amargautam/pakka/internal/statusline"
)

// receiptsLevel is the compression level RECEIPTS reports against — pakka's
// brand-default super-ultra. Used to resolve the measured output ratio.
const receiptsLevel = "super-ultra"

// Stats holds aggregated metrics from meter and audit data.
type Stats struct {
	SessionCount      int
	TotalTokensUsed   int64
	OutputTokensTotal int64
	TotalBytesSaved   int64
	TokensSavedEst    int64

	// Cache mix for the repo, from Claude Code transcripts. Used to price
	// input-side savings at the session's blended cache-aware rate rather than
	// a flat fresh-input rate. All zero when no transcript telemetry is found,
	// in which case input savings fall back to the flat fresh-input rate.
	InputTokens         int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	AuditEventCount     int
	ToolUseCounts       map[string]int // tool_name -> count
	GateVerdicts        int            // count of verdict files
	GatePassCount       int
	BugsCaught          int // error findings above threshold
	FirstSession        time.Time
	LastSession         time.Time

	// Output-reduction provenance, resolved from ~/.pakka/bench-ratios.json in
	// Gather. When OutputRatioMeasured is false the report falls back to the
	// per-level calibrated constant and discloses "default calibration".
	OutputRatioMeasured bool
	OutputReduction     float64 // measured reduction fraction, [0,1)
	OutputRatioSamples  int

	// Reviewer-gate calibration, resolved from the newest
	// benchmarks/results/calibration-*.json under repoRoot. When
	// CalibrationFound is false the review-gate calibration section reports
	// "unmeasured".
	CalibrationFound     bool
	CalibrationRecall    float64
	CalibrationPrecision float64
	CalibrationFPRate    float64
	CalibrationN         int
	CalibrationModel     string
	CalibrationDate      string
	// Degraded is true when a majority of scored seeds parsed no findings — the
	// rates are then annotated as untrustworthy rather than shown bare.
	CalibrationDegraded bool
	// Run health counts disclosed alongside the rates.
	CalibrationScored  int
	CalibrationTimeout int
	CalibrationErrors  int
}

// meterEntry mirrors one line in a meter JSONL file.
type meterEntry struct {
	TS             string `json:"ts"`
	SessionID      string `json:"session_id"`
	Repo           string `json:"repo,omitempty"`
	TokensUsed     int64  `json:"tokens_used"`
	BytesSaved     int64  `json:"bytes_saved"`
	TokensSavedEst int64  `json:"tokens_saved_est"`
	OutputTokens   int64  `json:"output_tokens,omitempty"`
}

// auditEntry mirrors one line in an audit JSONL file.
type auditEntry struct {
	Schema    string `json:"schema,omitempty"`
	TS        string `json:"ts,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Result    string `json:"result,omitempty"`
}

// verdictEntry mirrors one line in a verdict JSONL file.
type verdictEntry struct {
	TS      string `json:"ts"`
	Session string `json:"session"`
	Verdict string `json:"verdict"`
}

// Gather reads all JSONL files from meterDir and auditDir and returns Stats.
//
// repoRoot is the absolute path of the git repository root (or working
// directory). It is used to look up output tokens from Claude Code transcripts
// via the statusline package. Pass "" or "." to use the current directory.
//
// Purpose: Aggregate token usage, compression savings, tool counts, and gate
// verdicts from on-disk JSONL data.
// Errors: Returns error if neither meterDir nor auditDir can be read.
func Gather(meterDir, auditDir, repoRoot string) (*Stats, error) {
	s := &Stats{
		ToolUseCounts: make(map[string]int),
	}

	// Canonicalize repoRoot the same way meter tags entries (git toplevel, or
	// abs path when not a repo) so output-token snapshots can be matched to the
	// repo. Empty/"." means "no filter" — match all repos.
	canonRepo := ""
	if repoRoot != "" && repoRoot != "." {
		canonRepo = meter.RepoKey(repoRoot)
	}

	meterErr := gatherMeter(s, meterDir, canonRepo)
	auditErr := gatherAudit(s, auditDir)
	gatherVerdicts(s)

	// Cache mix from transcripts, so input-side savings can be priced at the
	// same blended cache-aware rate the status line uses. Best-effort: absence
	// (no repo, no transcripts, error) leaves the counts at zero and input
	// savings fall back to the flat fresh-input rate.
	if repoRoot != "" && repoRoot != "." {
		if in, cc, cr, err := statusline.RepoCacheMix("", repoRoot); err == nil {
			s.InputTokens, s.CacheCreationTokens, s.CacheReadTokens = in, cc, cr
		}
	}

	// Resolve the output-reduction ratio from measured bench data. RECEIPTS
	// reports the brand-default super-ultra level; the model is unknown here so
	// it is a wildcard (repo+level, then level). Absence leaves the report on
	// the calibrated constant. Best-effort: a load error is non-fatal.
	if store, err := benchratio.Load(); err == nil {
		if r, n, ok := store.Resolve(canonRepo, "", receiptsLevel); ok {
			s.OutputRatioMeasured = true
			s.OutputReduction = r
			s.OutputRatioSamples = n
		}
	}

	// Reviewer-gate calibration from the newest artifact under repoRoot's
	// benchmarks/results. Absence leaves the section "unmeasured".
	gatherCalibration(s, repoRoot)

	// If both dirs are unreadable, report an error.
	if meterErr != nil && auditErr != nil {
		return nil, fmt.Errorf("meter: %v; audit: %v", meterErr, auditErr)
	}

	return s, nil
}

// gatherCalibration loads the newest calibration-<date>.json and populates the
// Calibration* fields. Best-effort: no artifact / unreadable / malformed →
// leaves CalibrationFound false so the report shows "unmeasured".
//
// The artifact lives under the pakka repo's benchmarks/results, but the report
// is invoked with --repo-root pointing at the workspace parent (make
// self-report passes "..") whose meter/transcript keys differ. So we probe
// several candidate roots — the passed root, the CWD (where make runs), and the
// git toplevel — and take the newest artifact found across them.
func gatherCalibration(s *Stats, repoRoot string) {
	seen := map[string]bool{}
	var candidates []string
	add := func(root string) {
		if root == "" {
			return
		}
		dir := filepath.Join(root, "benchmarks", "results")
		if !seen[dir] {
			seen[dir] = true
			candidates = append(candidates, dir)
		}
	}
	add(repoRoot)
	if wd, err := os.Getwd(); err == nil {
		add(wd)
	}
	add(gitToplevel(repoRoot))

	newestName, newestDir := "", ""
	for _, dir := range candidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !strings.HasPrefix(n, "calibration-") || !strings.HasSuffix(n, ".json") {
				continue
			}
			// Filenames embed an ISO date (calibration-YYYY-MM-DD.json), so a
			// lexical compare picks the newest regardless of which dir it's in.
			if n > newestName {
				newestName, newestDir = n, dir
			}
		}
	}
	if newestName == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(newestDir, newestName))
	if err != nil {
		return
	}
	var art calibrate.Artifact
	if json.Unmarshal(data, &art) != nil {
		return
	}
	s.CalibrationFound = true
	s.CalibrationRecall = art.Aggregate.Recall
	s.CalibrationPrecision = art.Aggregate.Precision
	s.CalibrationFPRate = art.Aggregate.FPRate
	s.CalibrationN = art.Aggregate.N
	s.CalibrationModel = art.Aggregate.Model
	s.CalibrationDate = art.Date
	s.CalibrationDegraded = art.Aggregate.Degraded
	s.CalibrationScored = art.Aggregate.Counts.Scored
	s.CalibrationTimeout = art.Aggregate.Counts.Timeout
	s.CalibrationErrors = art.Aggregate.Counts.Error
}

// gitToplevel returns the git working-tree root containing root (or the CWD
// when root is empty/"."), or "" when not in a repo.
func gitToplevel(root string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	if root != "" && root != "." {
		cmd.Dir = root
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// repoMatches reports whether a meter entry's repo tag belongs to canonRepo
// (exact match, or a sub-repo nested under it). An empty canonRepo disables
// filtering and matches every entry.
func repoMatches(entryRepo, canonRepo string) bool {
	if canonRepo == "" {
		return true
	}
	return entryRepo == canonRepo || strings.HasPrefix(entryRepo, canonRepo+"/")
}

// gatherMeter accumulates meter stats. TokensUsed/BytesSaved/TokensSavedEst
// are per-event deltas and are summed across all entries. OutputTokens is
// different: each session-end entry is a repo-wide *cumulative snapshot*, so
// summing snapshots overcounts (100,250,450 sum to 800 but the true cumulative
// is 450). OutputTokensTotal therefore takes the MAX snapshot among entries
// matching canonRepo — the latest cumulative for the repo. (Owner decision
// 2026-06-11; see memory/DECISIONS.md "Output-tokens figure is repo-wide
// cumulative by design".)
func gatherMeter(s *Stats, dir, canonRepo string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		s.SessionCount++

		lines, err := data.ReadLines(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}

		for _, line := range lines {
			var me meterEntry
			if json.Unmarshal([]byte(line), &me) != nil {
				continue
			}
			s.TotalTokensUsed += me.TokensUsed
			if repoMatches(me.Repo, canonRepo) && me.OutputTokens > s.OutputTokensTotal {
				s.OutputTokensTotal = me.OutputTokens
			}
			s.TotalBytesSaved += me.BytesSaved
			s.TokensSavedEst += me.TokensSavedEst

			if me.TS != "" {
				if t, err := time.Parse(time.RFC3339Nano, me.TS); err == nil {
					if s.FirstSession.IsZero() || t.Before(s.FirstSession) {
						s.FirstSession = t
					}
					if s.LastSession.IsZero() || t.After(s.LastSession) {
						s.LastSession = t
					}
				}
			}
		}
	}
	return nil
}

func gatherAudit(s *Stats, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		lines, err := data.ReadLines(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}

		for _, line := range lines {
			var ae auditEntry
			if json.Unmarshal([]byte(line), &ae) != nil {
				continue
			}

			// Skip schema header line.
			if ae.Schema != "" {
				continue
			}

			s.AuditEventCount++
			if ae.Tool != "" {
				s.ToolUseCounts[ae.Tool]++
			}
		}
	}
	return nil
}

func gatherVerdicts(s *Stats) {
	entries, err := os.ReadDir(".pakka/reviews")
	if err != nil {
		return
	}

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "verdict-") || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		lines, err := data.ReadLines(filepath.Join(".pakka", "reviews", e.Name()))
		if err != nil {
			continue
		}

		for _, line := range lines {
			var ve verdictEntry
			if json.Unmarshal([]byte(line), &ve) != nil {
				continue
			}
			s.GateVerdicts++
			if ve.Verdict == "passed" {
				s.GatePassCount++
			}
		}
	}
}

// ResolveVersion resolves the plugin version for the RECEIPTS header from a
// .claude-plugin/plugin.json manifest, prefixed with "v" (e.g. "v0.19.0").
//
// Candidate roots are probed in order: the passed repoRoot, the CWD, and the
// CWD's git toplevel. The fallbacks matter because the production invocation
// (`make self-report`) passes --repo-root=.. — the workspace parent, which
// carries no manifest — while the invoking repo (the CWD) does.
//
// Purpose: Resolve the version at generation time instead of a hardcoded
// constant that goes stale between releases.
// Errors: None — missing/unparseable manifests or invalid version strings
// fall back to "unknown", never a stale literal.
func ResolveVersion(repoRoot string) string {
	seen := map[string]bool{}
	var candidates []string
	add := func(root string) {
		if root == "" || seen[root] {
			return
		}
		seen[root] = true
		candidates = append(candidates, root)
	}
	add(repoRoot)
	if wd, err := os.Getwd(); err == nil {
		add(wd)
		add(gitToplevel(wd))
	}
	for _, root := range candidates {
		if v := manifestVersion(filepath.Join(root, ".claude-plugin", "plugin.json")); v != "" {
			return "v" + v
		}
	}
	return "unknown"
}

// manifestVersionMax caps how much of a plugin manifest is read; anything
// larger is rejected outright.
const manifestVersionMax = 64 << 10 // 64 KiB

// versionPattern is the shape a manifest version must match to be rendered in
// the RECEIPTS header: alphanumeric start, then version-ish characters only.
// Anything else (newlines, backticks, markdown pipes, ...) could smuggle
// content into the generated markdown and is rejected.
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// manifestVersion reads a plugin.json and returns its validated "version"
// field, or "" when the file is missing, oversized, unparseable, or the
// version fails validation (empty, >64 chars, or not matching versionPattern).
func manifestVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, manifestVersionMax+1))
	if err != nil || len(raw) > manifestVersionMax {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return ""
	}
	v := manifest.Version
	if v == "" || len(v) > 64 || !versionPattern.MatchString(v) {
		return ""
	}
	return v
}

// FormatMarkdown renders Stats as a RECEIPTS.md markdown string.
//
// version is the already-resolved display string (e.g. "v0.19.0" or
// "unknown" from ResolveVersion) and is rendered verbatim.
//
// Purpose: Produce human-readable markdown summarizing build statistics.
// Errors: None (pure formatting).
func FormatMarkdown(s *Stats, version string) string {
	var b strings.Builder

	b.WriteString("# RECEIPTS.md — pakka built with pakka\n\n")
	b.WriteString(fmt.Sprintf("version: %s\n", version))
	b.WriteString(fmt.Sprintf("generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	// Build stats table.
	b.WriteString("## build stats\n\n")
	b.WriteString("| metric | value |\n")
	b.WriteString("|---|---|\n")
	b.WriteString(fmt.Sprintf("| sessions | %d |\n", s.SessionCount))

	if !s.FirstSession.IsZero() {
		b.WriteString(fmt.Sprintf("| first session | %s |\n", s.FirstSession.Format("2006-01-02")))
	}
	if !s.LastSession.IsZero() {
		b.WriteString(fmt.Sprintf("| last session | %s |\n", s.LastSession.Format("2006-01-02")))
	}

	b.WriteString(fmt.Sprintf("| total tokens used | %s |\n", fmtInt(s.TotalTokensUsed)))
	b.WriteString(fmt.Sprintf("| bytes saved (V2+V3+V4 compression) | %s |\n", fmtInt(s.TotalBytesSaved)))
	b.WriteString(fmt.Sprintf("| est. tokens saved (bytes ÷ 3.5) | %s |\n", fmtInt(s.TokensSavedEst)))

	// Output compression savings section.
	outputTokensEst := s.OutputTokensTotal
	// Resolve the reduction fraction most-specific-first: a measured bench
	// ratio (repo+level) when present, else the super-ultra calibrated constant
	// mult/(1+mult). ratioSource discloses provenance per spec AC3.
	mult := 1.94 // super-ultra calibrated multiplier (matches statusline.go)
	reduction := mult / (1 + mult)
	ratioSource := "default calibration"
	if s.OutputRatioMeasured {
		reduction = s.OutputReduction
		ratioSource = fmt.Sprintf("measured, n=%d", s.OutputRatioSamples)
	}
	outputTokensAvoided := int64(float64(outputTokensEst) * reduction)
	// Output is never cached — price it at the flat output rate.
	outputDollarSavings := float64(outputTokensAvoided) / 1_000_000 * pricing.Default.Output
	// Input savings are priced at the base input rate scaled by the session's
	// blended cache-aware multiplier: in a heavily cached repo most re-sent
	// tokens bill at the 0.1× cache-read rate, so a flat fresh-input rate
	// overstated input savings ~10×. With no transcript telemetry the multiplier
	// is 1.0 and this reduces to the old flat base-input-rate figure.
	effInput := statusline.BlendedInputMultiplier(s.InputTokens, s.CacheCreationTokens, s.CacheReadTokens)
	inputDollarSavings := float64(s.TokensSavedEst) / 1_000_000 * pricing.Default.Input * effInput
	totalDollarSavings := outputDollarSavings + inputDollarSavings

	b.WriteString("\n## output compression savings (V1 — calibrated bench)\n\n")
	b.WriteString("Output compression is the largest savings vector but cannot be measured from meter files alone — it requires comparing actual output tokens to a no-compression baseline. Calibrated 2026-05-02 by running benchmarks through Sonnet 4.6 at each level:\n\n")
	b.WriteString("| level | output reduction | est. cost saving |\n")
	b.WriteString("|---|---|---|\n")
	b.WriteString("| lite | ~27% | ~$0.68/MTok output |\n")
	b.WriteString("| strict | ~33% | ~$0.83/MTok output |\n")
	b.WriteString("| ultra | ~55% | ~$1.38/MTok output |\n")
	b.WriteString("| super-ultra | **~66%** | **~$1.65/MTok output** |\n")
	b.WriteString("\nAt Sonnet 4.6 pricing ($15/MTok output): super-ultra saves ~$9.90 per million output tokens vs uncompressed baseline.\n\n")
	b.WriteString("**Estimated total output savings across this build:**\n")
	b.WriteString(fmt.Sprintf("- Output tokens measured across %d sessions: %s\n", s.SessionCount, fmtInt(outputTokensEst)))
	b.WriteString(fmt.Sprintf("- Output-ratio source: %s\n", ratioSource))
	b.WriteString(fmt.Sprintf("- At %.0f%% output reduction (%s): ~%s tokens avoided\n", reduction*100, ratioSource, fmtInt(outputTokensAvoided)))
	b.WriteString(fmt.Sprintf("- At $15/MTok: **~$%.2f saved on output tokens alone**\n", outputDollarSavings))
	b.WriteString(fmt.Sprintf("- Input savings (V2+V3+V4, bytes_saved÷3.5 × $%.2f/MTok × %.2f blended cache rate): ~$%.2f\n", pricing.Default.Input, effInput, inputDollarSavings))
	b.WriteString(fmt.Sprintf("- **Total estimated savings: ~$%.2f**\n", totalDollarSavings))

	// Tool usage table.
	if len(s.ToolUseCounts) > 0 {
		b.WriteString("\n## tool usage\n\n")
		b.WriteString("| tool | calls |\n")
		b.WriteString("|---|---|\n")

		// Sort tools by count descending for stable output.
		type toolCount struct {
			name  string
			count int
		}
		var tools []toolCount
		for name, count := range s.ToolUseCounts {
			tools = append(tools, toolCount{name, count})
		}
		sort.Slice(tools, func(i, j int) bool {
			if tools[i].count != tools[j].count {
				return tools[i].count > tools[j].count
			}
			return tools[i].name < tools[j].name
		})
		for _, tc := range tools {
			b.WriteString(fmt.Sprintf("| %s | %s |\n", tc.name, fmtInt(int64(tc.count))))
		}
	}

	// Review gate table.
	b.WriteString("\n## review gate\n\n")
	b.WriteString("| metric | value |\n")
	b.WriteString("|---|---|\n")
	b.WriteString(fmt.Sprintf("| verdicts run | %d |\n", s.GateVerdicts))
	b.WriteString(fmt.Sprintf("| verdicts passed | %d |\n", s.GatePassCount))
	if s.GateVerdicts > 0 {
		rate := float64(s.GatePassCount) / float64(s.GateVerdicts) * 100
		b.WriteString(fmt.Sprintf("| pass rate | %.1f%% |\n", rate))
	} else {
		b.WriteString("| pass rate | — |\n")
	}

	// Review gate calibration section — measured reviewer precision/recall
	// against the seeded-bug corpus (spec 2026-07-27-reviewer-calibration).
	b.WriteString("\n## review gate calibration\n\n")
	if s.CalibrationFound {
		// The methodology sentence must not overclaim: when the artifact
		// carries no model, saying "rates carry ... model" would contradict the
		// table row below.
		if s.CalibrationModel != "" {
			b.WriteString("Measured reviewer recall/precision against the seeded-bug corpus " +
				"(benchmarks/seeds), scored by `pakka-core calibrate`. Rates carry n and model — no averaging across models.\n\n")
		} else {
			b.WriteString("Measured reviewer recall/precision against the seeded-bug corpus " +
				"(benchmarks/seeds), scored by `pakka-core calibrate`. Rates carry n; model is recorded when the headless runner reports it — no averaging across models.\n\n")
		}
		if s.CalibrationDegraded {
			// A majority of scored seeds parsed no findings: the rates reflect a
			// systemic parse/format failure, not reviewer quality. Say so
			// loudly instead of presenting bare numbers as performance.
			b.WriteString("> ⚠️ **DEGRADED RUN — rates are not trustworthy.** A majority of scored seeds returned a " +
				"non-empty reviewer response but zero parseable findings, the signature of a format/parse failure. " +
				"Treat the numbers below as a broken-harness signal, not reviewer performance. Re-run `make calibrate` " +
				"after fixing the finding format.\n\n")
		}
		b.WriteString("| metric | value |\n")
		b.WriteString("|---|---|\n")
		b.WriteString(fmt.Sprintf("| recall | %.0f%%%s |\n", s.CalibrationRecall*100, degradedMark(s.CalibrationDegraded)))
		b.WriteString(fmt.Sprintf("| precision | %.0f%%%s |\n", s.CalibrationPrecision*100, degradedMark(s.CalibrationDegraded)))
		b.WriteString(fmt.Sprintf("| false-positive rate | %.2f findings/clean-run |\n", s.CalibrationFPRate))
		b.WriteString(fmt.Sprintf("| n (bug seeds) | %d |\n", s.CalibrationN))
		b.WriteString(fmt.Sprintf("| seeds scored / timeout / error | %d / %d / %d |\n",
			s.CalibrationScored, s.CalibrationTimeout, s.CalibrationErrors))
		model := s.CalibrationModel
		if model == "" {
			model = "not recorded"
		}
		b.WriteString(fmt.Sprintf("| model | %s |\n", model))
		if s.CalibrationDate != "" {
			b.WriteString(fmt.Sprintf("| date | %s |\n", s.CalibrationDate))
		}
	} else {
		b.WriteString("Reviewer precision/recall is **unmeasured** — run `make calibrate` " +
			"(requires the claude CLI / OAuth session) to score the four reviewer agents against the seeded-bug corpus.\n")
	}

	b.WriteString("\n---\n\n")
	b.WriteString("Generated by `pakka-core report`. Apache-2.0.\n")

	return b.String()
}

// degradedMark returns a " ⚠️ degraded" suffix when the calibration run was
// flagged degraded, so the annotation rides alongside each rate cell (not only
// the banner) and a copy-pasted number can't shed its caveat.
func degradedMark(degraded bool) string {
	if degraded {
		return " ⚠️ degraded"
	}
	return ""
}

// fmtInt formats an integer with comma separators.
//
// Purpose: Produce human-readable numbers (e.g., 45200 -> "45,200").
// Errors: None.
func fmtInt(n int64) string {
	if n == math.MinInt64 {
		return "-9,223,372,036,854,775,808"
	}
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}
