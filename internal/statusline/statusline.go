// Package statusline prints a compact pakka session summary to stdout.
//
// Metrics are aggregated cumulative-per-repo:
//   - Input tokens come from every transcript under ~/.claude/projects/ whose
//     decoded cwd resolves (via git toplevel) to the same repo as the current
//     session. Cost-weighted: input × 1× + cache_creation × 1.25× +
//     cache_read × 0.1×, plus tokensSavedEst × 1× as the savings numerator.
//   - Output tokens are summed across the same transcripts. Output savings
//     remain the mode coefficient (lite/strict/ultra/audit).
//   - tokensSavedEst is summed across every meter file with matching repo tag.
//   - bugsCaught is an all-time count across the repo's review findings.
//
// Override hooks for tests:
//
//	statusline.OverrideHome — points at a fake $HOME (meter dir + projects dir).
//	statusline.OverrideRepoKey — substitute repo identity (no git invocation).
package statusline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/amargautam/pakka/internal/hookevent"
	"github.com/amargautam/pakka/internal/meter"
)

// Cost weights reflect Anthropic billing ratios. Input baseline 1×;
// cache_creation 1.25×; cache_read 0.1×. Numerator (tokensSavedEst) is 1×
// because truncated tool result bytes would have been fresh input.
const (
	costWeightInput          = 1.0
	costWeightCacheCreation  = 1.25
	costWeightCacheRead      = 0.1
)

// OverrideHome, when non-empty, substitutes for os.UserHomeDir(). Used by
// tests to redirect ~/.pakka/meter and ~/.claude/projects lookups.
var OverrideHome string

// OverrideRepoKey, when non-nil, replaces meter.RepoKey for repo resolution.
// Used by tests to avoid invoking git on synthetic paths.
var OverrideRepoKey func(cwd string) string

// Uncalibrated multipliers for v0.1.0. Pass 5 bench replaces these with measured values.
var outputMultiplier = map[string]float64{
	"lite":   0.11,
	"strict": 0.33,
	"ultra":  0.67,
	"audit":  0.0,
}

// metrics holds computed status-line values.
type metrics struct {
	compressMode    string
	inSavedTokens   int64 // tokensSavedEst summed across repo's meter files
	inCostUnits     int64 // cost-weighted input denominator
	inPct           int64
	outTokens       int64 // assistant output tokens summed across repo's transcripts
	outSavedEst     int64
	outPct          int64
	bugsCaught      int
}

// pctRound returns round(num*100/denom) as int64. Returns 0 when denom <= 0.
// Uses math.Round so 0.4→0, 0.5→1, 24.6→25.
func pctRound(num, denom int64) int64 {
	if denom <= 0 {
		return 0
	}
	return int64(math.Round(float64(num) * 100 / float64(denom)))
}

// resolveHome returns OverrideHome if set, else os.UserHomeDir().
func resolveHome() string {
	if OverrideHome != "" {
		return OverrideHome
	}
	h, _ := os.UserHomeDir()
	return h
}

// resolveRepoKey returns OverrideRepoKey(cwd) if set, else meter.RepoKey.
func resolveRepoKey(cwd string) string {
	if OverrideRepoKey != nil {
		return OverrideRepoKey(cwd)
	}
	return meter.RepoKey(cwd)
}

// compute gathers all status-line metrics from disk.
func compute(event *hookevent.Event, compressMode string) metrics {
	if compressMode == "" {
		compressMode = "strict"
	}

	cwd := event.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	repo := resolveRepoKey(cwd)

	home := resolveHome()
	meterDir := filepath.Join(home, ".pakka", "meter")
	projectsDir := filepath.Join(home, ".claude", "projects")

	// Cumulative meter savings across all sessions for this repo.
	savedTokens := readAllMeter(meterDir, repo)

	// Cumulative transcript input/output across all sessions for this repo.
	inTokens, cacheCreation, cacheRead, outTokens := readAllTranscripts(projectsDir, repo)

	// Cost-weighted input denominator. Numerator (savedTokens) keeps 1× weight.
	costUnits := int64(math.Round(
		float64(inTokens)*costWeightInput +
			float64(cacheCreation)*costWeightCacheCreation +
			float64(cacheRead)*costWeightCacheRead +
			float64(savedTokens)*costWeightInput,
	))
	inPct := pctRound(savedTokens, costUnits)

	mult := outputMultiplier[compressMode]
	outSaved := int64(float64(outTokens) * mult)
	outPct := pctRound(outSaved, outTokens+outSaved)

	// All-time bug count across the repo's review findings.
	bugs := countBugsCaught(filepath.Join(repo, ".pakka", "reviews"))

	return metrics{
		compressMode:  compressMode,
		inSavedTokens: savedTokens,
		inCostUnits:   costUnits,
		inPct:         inPct,
		outTokens:     outTokens,
		outSavedEst:   outSaved,
		outPct:        outPct,
		bugsCaught:    bugs,
	}
}

// utf8Capable reports whether the terminal locale supports UTF-8.
func utf8Capable() bool {
	for _, k := range []string{"LC_ALL", "LANG", "LC_CTYPE"} {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		lv := strings.ToLower(v)
		if strings.Contains(lv, "utf-8") || strings.Contains(lv, "utf8") {
			return true
		}
	}
	return false
}

// humanize formats n as a compact human-readable token count.
//
// Rules:
//   - n < 1000      → raw integer ("0", "999")
//   - 1_000 ≤ n < 1_000_000 → one decimal "K", floor-truncated ("1.0K", "12.4K")
//   - n ≥ 1_000_000 → one decimal "M", floor-truncated ("1.2M")
//
// Floor (not round) is intentional: predictable, never inflates a count,
// matches `du -h` semantics. Boundary: 999 → "999", 1000 → "1.0K".
// Negative inputs are clamped to "0" — the metrics this serves are
// non-negative by construction (counters), so a negative value indicates
// upstream bug, not signal we want to render.
func humanize(n int64) string {
	if n < 0 {
		return "0"
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		// Floor to one decimal: integer divide tenths.
		tenths := n / 100 // e.g. 12450 → 124
		return fmt.Sprintf("%d.%dK", tenths/10, tenths%10)
	}
	tenths := n / 100_000 // e.g. 1234567 → 12
	return fmt.Sprintf("%d.%dM", tenths/10, tenths%10)
}

// formatLine renders the status-line body using the supplied glyphs.
//
// Format: absolute saved tokens (humanized) + percent in parens, both shown.
// Percent alone is meaningless without scale (50% of 200 vs 50% of 200K), so
// the body always carries both. Unmeasured values render as "0 (0%)".
//
// UTF-8: [strict] · ↑12.4K (43%) / ↓7.1K (33%) tok saved · 0 bugs caught
// ASCII: [strict] | in 12.4K (43%) / out 7.1K (33%) tok saved | 0 bugs caught
func formatLine(m metrics, inArrow, outArrow, sep string) string {
	return fmt.Sprintf("[%s] %s %s%s (%d%%) / %s%s (%d%%) tok saved %s %d bugs caught",
		m.compressMode, sep,
		inArrow, humanize(m.inSavedTokens), m.inPct,
		outArrow, humanize(m.outSavedEst), m.outPct,
		sep, m.bugsCaught)
}

// Run prints the pakka status line to w.
//
// Format (UTF-8): pakka [strict] · ↑12.4K (43%) / ↓7.1K (33%) tok saved · 0 bugs caught
// Format (ascii): pakka [strict] | in 12.4K (43%) / out 7.1K (33%) tok saved | 0 bugs caught
//
// Both absolute saved-token counts (humanized to K/M, floor-truncated) and
// percentages are shown — percent alone is meaningless without scale.
//
// Arrows follow conventional download/upload semantics: ↑ = input going UP to
// the API, ↓ = output coming DOWN.
//
// Purpose: Emit compact one-line session summary for Claude Code's statusLine display.
// Errors: Returns error only on write failure to w.
func Run(event *hookevent.Event, w io.Writer, compressMode string) error {
	m := compute(event, compressMode)
	utf8 := utf8Capable()

	var inArrow, outArrow, sep string
	if utf8 {
		inArrow, outArrow, sep = "↑", "↓", "·"
	} else {
		inArrow, outArrow, sep = "in ", "out ", "|"
	}

	body := formatLine(m, inArrow, outArrow, sep)
	_, err := fmt.Fprintf(w, "\033[38;2;245;158;11mpakka\033[0m %s", body)
	return err
}

// Summary returns the plain-text status line (no ANSI escapes).
// Suitable for embedding in commit trailers.
func Summary(event *hookevent.Event, compressMode string) string {
	m := compute(event, compressMode)
	utf8 := utf8Capable()

	var inArrow, outArrow, sep string
	if utf8 {
		inArrow, outArrow, sep = "↑", "↓", "·"
	} else {
		inArrow, outArrow, sep = "in ", "out ", "|"
	}
	return formatLine(m, inArrow, outArrow, sep)
}

// countBugsCaught scans findings JSONL files (not verdict-* files) in dir
// and counts entries with severity=error and confidence >= 80.
//
// All-time count: no time filter. Per-repo isolation comes from `dir` being
// scoped to a specific <repo>/.pakka/reviews directory.
func countBugsCaught(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	count := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		// Skip verdict files — they contain pass/fail verdicts, not findings.
		if strings.HasPrefix(name, "verdict-") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var f struct {
				Severity   string `json:"severity"`
				Confidence int    `json:"confidence"`
			}
			if json.Unmarshal([]byte(line), &f) != nil {
				continue
			}
			if f.Severity == "error" && f.Confidence >= 80 {
				count++
			}
		}
	}
	return count
}

func shortSID(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}

// meterEntry matches the JSONL written by the meter package.
type meterEntry struct {
	Repo           string `json:"repo"`
	TokensSavedEst int64  `json:"tokens_saved_est"`
}

// readAllMeter walks meterDir, reads every .jsonl file, and sums
// tokens_saved_est across entries whose `repo` field matches the supplied
// repo. Legacy entries (no repo field) are skipped.
//
// Returns 0 when meterDir is missing or unreadable.
func readAllMeter(meterDir, repo string) (savedTokens int64) {
	if repo == "" {
		return 0
	}
	entries, err := os.ReadDir(meterDir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(meterDir, e.Name())
		savedTokens += sumMeterFile(path, repo)
	}
	return savedTokens
}

// sumMeterFile returns the sum of tokens_saved_est across entries in path
// whose `repo` field equals the supplied repo.
func sumMeterFile(path, repo string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var total int64
	for sc.Scan() {
		var e meterEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if e.Repo != repo {
			continue
		}
		total += e.TokensSavedEst
	}
	return total
}

// decodeProjectDir converts a Claude Code project subdir name back into a
// best-effort original cwd. Claude encodes both '/' AND '.' as '-' in the
// directory name, so the mapping is genuinely ambiguous; this helper exists
// for tests where we know the encoding fits the simple '/'→'-' form.
//
// readAllTranscripts does NOT rely on this for production resolution; it
// reads the literal `cwd` field embedded in transcript lines instead.
func decodeProjectDir(name string) string {
	if name == "" {
		return ""
	}
	return strings.ReplaceAll(name, "-", "/")
}

// readProjectCWD scans transcript files in dir for the first line carrying a
// `cwd` field. Claude Code embeds the original working directory in many
// event lines (permission-mode, user, assistant, etc.), which lets us
// resolve a project subdir back to its real cwd unambiguously — sidestepping
// the dash-encoding ambiguity (since '.' and '/' both encode to '-').
//
// Returns "" when no transcript yields a cwd.
func readProjectCWD(dir string) string {
	files, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, f.Name())
		fp, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fp)
		buf := make([]byte, 0, 64*1024)
		sc.Buffer(buf, 4*1024*1024)
		for sc.Scan() {
			var probe struct {
				CWD string `json:"cwd"`
			}
			if json.Unmarshal(sc.Bytes(), &probe) == nil && probe.CWD != "" {
				fp.Close()
				return probe.CWD
			}
		}
		fp.Close()
	}
	return ""
}

// readAllTranscripts walks projectsDir, resolves each subdirectory to its
// real cwd by reading the embedded `cwd` field from the transcripts inside,
// computes the repo key, and (if it matches the supplied repo) sums
// input/output usage across every transcript .jsonl in that subdirectory.
//
// Falls back to a naive '-'→'/' decoding when no transcript exposes a cwd
// field (older Claude Code versions). The literal absolute fallback rarely
// matches a real repo, so most foreign dirs are silently skipped.
func readAllTranscripts(projectsDir, repo string) (in, cacheCreation, cacheRead, out int64) {
	if repo == "" {
		return 0, 0, 0, 0
	}
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return 0, 0, 0, 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, e.Name())

		// Prefer the embedded cwd from transcript contents (unambiguous);
		// fall back to dash-decoding the directory name (ambiguous, used
		// in tests with synthetic transcripts that don't include a cwd).
		cwd := readProjectCWD(dirPath)
		if cwd == "" {
			cwd = decodeProjectDir(e.Name())
		}
		if cwd == "" {
			continue
		}
		if resolveRepoKey(cwd) != repo {
			continue
		}

		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			ti, tcc, tcr, to := sumTranscriptFile(filepath.Join(dirPath, f.Name()))
			in += ti
			cacheCreation += tcc
			cacheRead += tcr
			out += to
		}
	}
	return in, cacheCreation, cacheRead, out
}

// transcriptUsage matches the two candidate JSON shapes Claude Code emits.
type transcriptUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// sumTranscriptFile sums usage fields across all entries in a transcript.
//
// Returns (in, cacheCreation, cacheRead, out). Tolerates partial/missing
// fields and the two candidate JSON shapes (message.usage vs top-level usage).
func sumTranscriptFile(path string) (in, cacheCreation, cacheRead, out int64) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var u transcriptUsage
		// Shape A: {"message":{"usage":{...}}}
		var a struct {
			Message struct {
				Usage transcriptUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &a) == nil &&
			(a.Message.Usage.InputTokens|a.Message.Usage.OutputTokens|
				a.Message.Usage.CacheReadInputTokens|a.Message.Usage.CacheCreationInputTokens) != 0 {
			u = a.Message.Usage
		} else {
			// Shape B: top-level {"usage":{...}}
			var b struct {
				Usage transcriptUsage `json:"usage"`
			}
			if json.Unmarshal(line, &b) != nil {
				continue
			}
			u = b.Usage
		}
		in += u.InputTokens
		cacheCreation += u.CacheCreationInputTokens
		cacheRead += u.CacheReadInputTokens
		out += u.OutputTokens
	}
	return in, cacheCreation, cacheRead, out
}
