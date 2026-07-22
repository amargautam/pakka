// Package report reads meter and audit JSONL files and produces aggregate
// build statistics for RECEIPTS.md output.
package report

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/benchratio"
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

	// If both dirs are unreadable, report an error.
	if meterErr != nil && auditErr != nil {
		return nil, fmt.Errorf("meter: %v; audit: %v", meterErr, auditErr)
	}

	return s, nil
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

// FormatMarkdown renders Stats as a RECEIPTS.md markdown string.
//
// Purpose: Produce human-readable markdown summarizing build statistics.
// Errors: None (pure formatting).
func FormatMarkdown(s *Stats, version string) string {
	var b strings.Builder

	b.WriteString("# RECEIPTS.md — pakka built with pakka\n\n")
	b.WriteString(fmt.Sprintf("version: v%s\n", version))
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

	b.WriteString("\n---\n\n")
	b.WriteString("Generated by `pakka-core report`. Apache-2.0.\n")

	return b.String()
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
