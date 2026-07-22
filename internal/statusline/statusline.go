// Package statusline prints a compact pakka session summary to stdout.
//
// Calibrated 2026-05-02. See benchmarks/compress-samples/ for bench data.
//
// Metrics are aggregated cumulative-per-repo:
//   - Input tokens come from every transcript under ~/.claude/projects/ whose
//     decoded cwd resolves (via git toplevel) to the same repo as the current
//     session. Cost-weighted: input × 1× + cache_creation × 1.25× +
//     cache_read × 0.1×, plus tokensSavedEst × 1× as the savings numerator.
//   - Output tokens are summed across the same transcripts. Output savings
//     ARE rendered as a percentage Y%, derived from the level's
//     outputMultiplier as round(mult/(1+mult)*100). Y% reflects calibrated
//     bench measurements (2026-05-02, Sonnet 4.6 + Opus 4.5). The output % is
//     still level-derived pending the baseline-vs-compressed bench; the input
//     side of the $ estimate is now per-session measured — saved input tokens
//     are priced at the session's blended cache-aware rate (see
//     BlendedInputMultiplier), not a flat fresh-input rate.
//     Decision: memory/DECISIONS.md "Status-line format (decided 2026-04-29 by user)".
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
	"github.com/amargautam/pakka/internal/pricing"
)

// Cost weights reflect Anthropic billing ratios. Input baseline 1×;
// cache_creation 1.25×; cache_read 0.1×. Numerator (tokensSavedEst) is 1×
// because truncated tool result bytes would have been fresh input.
const (
	costWeightInput         = 1.0
	costWeightCacheCreation = 1.25
	costWeightCacheRead     = 0.1
)

// BlendedInputMultiplier returns the cache-aware effective input-rate
// multiplier for a session, given its actual telemetry: fresh input,
// cache-creation, and cache-read token counts. It is the token-weighted mean
// of the three cost weights:
//
//	(in×1.0 + cc×1.25 + cr×0.1) / (in + cc + cr)
//
// Pricing saved input tokens at base_input_price × this multiplier makes the
// reported $ figure reflect what a cached environment actually pays: ~0.19× in
// a 90%-cache-read env (0.9×0.1 + 0.1×1.0), ~1× in an all-fresh env. When the
// denominator is zero
// (no telemetry — e.g. no transcript found) it falls back to 1.0, i.e. flat
// fresh-input pricing, preserving the pre-cache-aware behavior.
func BlendedInputMultiplier(in, cacheCreation, cacheRead int64) float64 {
	denom := float64(in + cacheCreation + cacheRead)
	if denom <= 0 {
		return 1.0
	}
	return (float64(in)*costWeightInput +
		float64(cacheCreation)*costWeightCacheCreation +
		float64(cacheRead)*costWeightCacheRead) / denom
}

// OverrideHome, when non-empty, substitutes for os.UserHomeDir(). Used by
// tests to redirect ~/.pakka/meter and ~/.claude/projects lookups.
var OverrideHome string

// OverrideRepoKey, when non-nil, replaces meter.RepoKey for repo resolution.
// Used by tests to avoid invoking git on synthetic paths.
var OverrideRepoKey func(cwd string) string

// outputMultiplier maps output compression level to an estimated savings ratio.
//
// Calibrated 2026-05-02 against Sonnet 4.6 + Opus 4.5 on
// benchmarks/compress-samples/subagent-return.txt. Reduction measured as
// (baseline_output_tokens - compressed_output_tokens) / baseline_output_tokens.
// Output-side only: replace with a per-session measured ratio when the
// baseline-vs-compressed output bench lands. (The input side of the $ estimate
// is already per-session measured — see BlendedInputMultiplier.)
//
// TODO(issue #13): this constant table MUST be replaced by the measured
// per-level output ratio from a full `make bench` run (A/B via claude -p
// OAuth, raw arm = PAKKA_DISABLED=1). The harness is in internal/bench and
// emits measured_output_ratio per level into benchmarks/results/. A smoke
// run is not statistically sufficient to replace these values; the full
// corpus run is. Do not delete this TODO until the table values carry
// commit-hash provenance from benchmarks/results/.
var outputMultiplier = map[string]float64{
	"lite":        0.37,
	"strict":      0.49,
	"ultra":       1.22,
	"super-ultra": 1.94,
}

// transcriptCacheEntry holds pre-summed token counts for one transcript file.
type transcriptCacheEntry struct {
	Mtime         int64 `json:"mtime"`
	Size          int64 `json:"size"`
	InTokens      int64 `json:"in_tokens"`
	CacheCreation int64 `json:"cache_creation"`
	CacheRead     int64 `json:"cache_read"`
	OutTokens     int64 `json:"out_tokens"`
}

type transcriptCache struct {
	Entries map[string]transcriptCacheEntry `json:"entries"`
}

func loadTranscriptCache(path string) *transcriptCache {
	c := &transcriptCache{Entries: make(map[string]transcriptCacheEntry)}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, c)
	if c.Entries == nil {
		c.Entries = make(map[string]transcriptCacheEntry)
	}
	return c
}

func saveTranscriptCache(path string, c *transcriptCache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// meterCacheEntry holds per-repo pre-summed tokens_saved_est for one meter
// file, keyed by the file's mtime+size. Sums is a repo→sum map so a workspace
// with several repos doesn't thrash the cache when the status line is rendered
// from different repos in turn.
type meterCacheEntry struct {
	Mtime int64            `json:"mtime"`
	Size  int64            `json:"size"`
	Sums  map[string]int64 `json:"sums"`
}

type meterCache struct {
	Entries map[string]meterCacheEntry `json:"entries"`
}

func loadMeterCache(path string) *meterCache {
	c := &meterCache{Entries: make(map[string]meterCacheEntry)}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, c)
	if c.Entries == nil {
		c.Entries = make(map[string]meterCacheEntry)
	}
	return c
}

func saveMeterCache(path string, c *meterCache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// metrics holds computed status-line values.
type metrics struct {
	outputLevel   string
	inSavedTokens int64 // tokensSavedEst summed across repo's meter files
	inCostUnits   int64 // cost-weighted input denominator
	inPct         int64
	outTokens     int64   // assistant output tokens summed across repo's transcripts
	outPct        int64   // level-derived placeholder: round(mult/(1+mult)*100)
	savedUSD      float64 // estimated USD saved (input + output sides, using pricing.Default)
	bugsCaught    int
	staleCompress int // orchestrator entries with validatorPasses=false

	// Context-window usage, sourced from the CC 2.1 native statusLine payload
	// (context_window.current_usage). hasCtx is false on pre-2.1 payloads and
	// in the documented null states (before first API call, right after
	// /compact) — the ctx segment is then omitted entirely, never blank.
	hasCtx    bool
	ctxTokens int64 // input + cache_creation + cache_read (docs' input-only formula)
	ctxPct    int64
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
//
// Default level is "super-ultra" — pakka's brand default. See
// memory/DECISIONS.md "Default output level: super-ultra".
// Invalid levels also fall back to "super-ultra" so a stale config never
// silently downgrades compression below the brand baseline.
//
// stale is the pre-computed orchestrator stale count, supplied by the caller
// (main.go) to keep statusline free of compress/orchestrator coupling.
//
// native carries the CC 2.1 statusLine payload fields (nil on pre-2.1 hooks
// and for Summary's commit-trailer path). It supplies ONLY the context-usage
// segment (context_window.current_usage); all cumulative figures — $ estimate,
// savings %, in/out tokens — always come from the transcript scan, never the
// session-scoped payload (see compute body; #34).
func compute(event *hookevent.Event, native *NativePayload, outputLevel string, stale int) metrics {
	outputLevel = resolveLevel(outputLevel)

	cwd := event.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	repo := resolveRepoKey(cwd)

	home := resolveHome()
	meterDir := filepath.Join(home, ".pakka", "meter")
	projectsDir := filepath.Join(home, ".claude", "projects")

	// Cumulative meter savings across all sessions for this repo.
	// pakka-specific — no native statusLine field carries this; always read.
	savedTokens := readAllMeter(meterDir, repo)

	// Token usage feeding the $ estimate, savings-% denominator, and in/out
	// figures is ALWAYS repo-cumulative: summed across every transcript for
	// this repo via the mtime+size-cached JSONL scan (the cache bounds the
	// per-render cost). The CC 2.1 native payload is context-window-scoped and
	// therefore feeds ONLY the ctx segment (nativeContextUsage below) — never
	// these cumulative figures. Repo-cumulative is the locked decision
	// (DECISIONS.md #11, v0.10.0); #34 was a #30 regression that sourced these
	// from the session-scoped native payload.
	inTokens, cacheCreation, cacheRead, outTokens := readAllTranscripts(projectsDir, repo)

	// Cost-weighted input denominator = actual spend only. savedTokens is the
	// numerator (savings) and must not appear in the denominator, otherwise
	// the savings % is understated.
	costUnits := int64(math.Round(
		float64(inTokens)*costWeightInput +
			float64(cacheCreation)*costWeightCacheCreation +
			float64(cacheRead)*costWeightCacheRead,
	))
	inPct := pctRound(savedTokens, costUnits)

	// outPct is a level-derived placeholder until v0.2.0 measurement lands.
	// See package doc + outputMultiplier comment.
	mult := outputMultiplier[outputLevel]
	outPct := int64(math.Round(mult / (1 + mult) * 100))

	// All-time bug count across the repo's review findings (root + sub-repos).
	bugs := countAllBugsCaught(repo)

	// Estimated USD saved using pricing.Default (Sonnet 4.6) since we don't
	// know the model from meter/transcripts.
	//
	// Input savings: truncated bytes that would have been fresh input tokens.
	// Priced cache-aware: base input price × the session's blended effective
	// input rate (BlendedInputMultiplier over the observed fresh/cache-write/
	// cache-read mix). In a heavily cached session most re-sent tokens bill at
	// the 0.1× cache-read rate, so pricing every saved token at the full fresh
	// rate overstated input savings ~10×. With no telemetry the multiplier is
	// 1.0 and this reduces to the old flat-rate figure.
	// inputSavedUSD = savedTokens / 1M * Input_price * effInput
	//
	// Output savings: calibrated reduction fraction applied to observed output
	// volume. Output is never cached, so it stays priced at the flat output rate.
	// calibratedRatio = mult / (1 + mult) — see outputMultiplier doc.
	// outputSavedUSD = outTokens * calibratedRatio / 1M * Output_price
	//
	// Note: outTokens is the observed/compressed value; the formula treats it as
	// baseline per the spec. This yields a conservative (under) estimate of savings
	// by factor (1+mult) — see spec comment in task brief.
	prices := pricing.Default
	effInput := BlendedInputMultiplier(inTokens, cacheCreation, cacheRead)
	inputSavedUSD := float64(savedTokens) / 1_000_000 * prices.Input * effInput
	calibratedRatio := mult / (1 + mult)
	outputSavedUSD := float64(outTokens) * calibratedRatio / 1_000_000 * prices.Output
	totalSavedUSD := inputSavedUSD + outputSavedUSD

	hasCtx, ctxTokens, ctxPct := nativeContextUsage(native)

	return metrics{
		hasCtx:        hasCtx,
		ctxTokens:     ctxTokens,
		ctxPct:        ctxPct,
		outputLevel:   outputLevel,
		inSavedTokens: savedTokens,
		inCostUnits:   costUnits,
		inPct:         inPct,
		outTokens:     outTokens,
		outPct:        outPct,
		savedUSD:      totalSavedUSD,
		bugsCaught:    bugs,
		staleCompress: stale,
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
// Input side: absolute saved tokens (humanized) + percent X% in parens. X%
// is meaningful — the meter records real bytes truncated.
//
// Output side: absolute output volume + percent Y% in parens. Y% is
// LEVEL-DERIVED placeholder (round(mult/(1+mult)*100)) until v0.2.0
// baseline-vs-compressed bench lands. See outputMultiplier doc.
//
// UTF-8: [ultra] · ↑12.4K (43%) / ↓7.0K (40%) tokens saved · 0 bugs caught
// ASCII: [ultra] | in 12.4K (43%) / out 7.0K (40%) tokens saved | 0 bugs caught
//
// Bracket label reflects the active output compression level; "ultra" is
// the default tier per DECISIONS.md.
func formatLine(m metrics, inArrow, outArrow, sep string) string {
	staleSeg := ""
	if m.staleCompress > 0 {
		staleSeg = fmt.Sprintf(" %s ! %d stale", sep, m.staleCompress)
	}
	return fmt.Sprintf("[%s] %s %s%s (%d%%) / %s%s (%d%%) tokens saved %s %d bugs caught%s",
		m.outputLevel, sep,
		inArrow, humanize(m.inSavedTokens), m.inPct,
		outArrow, humanize(m.outTokens), m.outPct,
		sep, m.bugsCaught, staleSeg)
}

// formatRunLine renders the compact dollar-savings status-line body used by Run().
//
// Format: [<level>] <sep> ~$X.XX saved (est) <sep> N bugs caught[stale-segment]
//
// The dollar amount is formatted with FormatUSD (2 decimal places, "$" prefix).
// The figure is a cumulative-per-repo estimate, NOT a metered total: the input
// side is real (bytes truncated, from the meter) but the output side multiplies
// observed output volume by a constant per-level ratio (outputMultiplier) that
// is not yet measured against a no-compression baseline. The "~" tilde and the
// explicit "(est)" tag mark it as modeled so the figure is never read as a
// to-the-cent measurement — that honesty is the point (see #11 / brand voice).
// Replace the "(est)" qualifier with a measured ratio when the baseline bench
// lands (DECISIONS.md "Bench methodology").
// Stale segment appended only when staleCompress > 0.
// ANSI 24-bit color: ctx in cyan (94,193,255), savings in green (111,208,140),
// bugs in red (232,99,74).
//
// Ctx segment: "ctx <abs> (<pct>%)" — context-window usage from the CC 2.1
// native payload, absolute tokens AND percent together (hard rule: never
// percent alone). Omitted entirely — never blank — when the payload carries
// no usable context_window data (pre-2.1 CC, pre-first-API-call, post-/compact).
func formatRunLine(m metrics, sep string) string {
	ctxSeg := ""
	if m.hasCtx {
		ctxSeg = fmt.Sprintf("\033[38;2;94;193;255mctx %s (%d%%)\033[0m %s ", humanize(m.ctxTokens), m.ctxPct, sep)
	}
	staleSeg := ""
	if m.staleCompress > 0 {
		staleSeg = fmt.Sprintf(" %s ! %d stale", sep, m.staleCompress)
	}
	savedStr := fmt.Sprintf("\033[38;2;111;208;140m~%s saved (est)\033[0m", pricing.FormatUSD(m.savedUSD))
	bugsStr := fmt.Sprintf("\033[38;2;232;99;74m%d bugs caught\033[0m", m.bugsCaught)
	return fmt.Sprintf("\033[38;2;245;158;11m[%s]\033[0m %s %s%s %s %s%s",
		m.outputLevel, sep, ctxSeg, savedStr, sep, bugsStr, staleSeg)
}

// resolveLevel returns a valid compression level from the supplied string.
// Empty string and unknown levels both fall back to "super-ultra" — pakka's brand
// default per memory/DECISIONS.md "Default output level: super-ultra".
func resolveLevel(outputLevel string) string {
	if outputLevel == "" {
		return "super-ultra"
	}
	if _, ok := outputMultiplier[outputLevel]; !ok {
		return "super-ultra"
	}
	return outputLevel
}

// Run prints the compact pakka status line to w, with ANSI colour on the "pakka" label.
//
// Format: pakka [<level>] · ctx <abs> (<pct>%) · ~$X.XX saved (est) · N bugs caught
//
// The ctx segment is sourced from the CC 2.1 native statusLine payload
// (context_window.current_usage) — pass native=nil for pre-2.1 hooks; the
// segment is then omitted and the line matches the pre-2.1 format.
//
// Replaces the old ↑/↓ token-arrow format with a dollar-savings estimate.
// Separator (· or |) follows UTF-8 locale detection. No token arrows emitted.
//
// stale > 0 appends "· ! N stale" — same semantics as Summary().
//
// Bracket label is the output compression level (lite|strict|ultra|super-ultra).
// "super-ultra" is the default tier — pakka's brand thesis is fewer tokens, and
// the default reflects it. See memory/DECISIONS.md.
//
// Purpose: Emit compact dollar-savings line for Claude Code's statusLine display.
// Errors: Returns error only on write failure to w.
func Run(event *hookevent.Event, native *NativePayload, w io.Writer, outputLevel string, stale int) error {
	m := compute(event, native, outputLevel, stale)
	var sep string
	if utf8Capable() {
		sep = "·"
	} else {
		sep = "|"
	}
	_, err := fmt.Fprintf(w, "\033[38;2;245;158;11mpakka\033[0m %s", formatRunLine(m, sep))
	return err
}

// Summary returns the plain-text status line (no ANSI escapes).
// Suitable for embedding in commit trailers.
//
// stale is the pre-computed orchestrator stale count. Callers obtain it via
// orchestrator.CountStaleFromDisk so statusline stays free of compress coupling.
//
// Summary always uses the transcript-scan path (native=nil): its callers are
// hook events (commit trailer) that carry no CC 2.1 statusLine payload, and
// its cumulative-per-repo semantics must not silently narrow to one context
// window.
func Summary(event *hookevent.Event, outputLevel string, stale int) string {
	m := compute(event, nil, outputLevel, stale)
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

// countAllBugsCaught counts bugs at root/.pakka/reviews and at each immediate
// child directory's .pakka/reviews. One level deep only. Skips dirs starting
// with "." and dirs named node_modules, vendor, __pycache__.
func countAllBugsCaught(root string) int {
	total := countBugsCaught(filepath.Join(root, ".pakka", "reviews"))

	entries, err := os.ReadDir(root)
	if err != nil {
		return total
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		switch name {
		case "node_modules", "vendor", "__pycache__":
			continue
		}
		total += countBugsCaught(filepath.Join(root, name, ".pakka", "reviews"))
	}
	return total
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
// Per-file sums are memoized in ~/.pakka/meter-cache.json keyed by mtime+size,
// mirroring the transcript cache. Meter files accumulate one-per-session and
// are never pruned (1000+ observed), so without the cache the status line —
// rendered on every turn — re-scanned every file each render, an O(sessions)
// cost that grows unbounded. With the cache only the current session's file
// (whose size changes as entries append) is re-summed per render.
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

	cachePath := filepath.Join(resolveHome(), ".pakka", "meter-cache.json")
	cache := loadMeterCache(cachePath)
	dirty := false

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(meterDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		mtime := info.ModTime().UnixNano()
		size := info.Size()

		ce, ok := cache.Entries[path]
		fresh := ok && ce.Mtime == mtime && ce.Size == size
		if !fresh {
			// File new or changed — discard stale per-repo sums.
			ce = meterCacheEntry{Mtime: mtime, Size: size, Sums: map[string]int64{}}
		}
		if sum, hit := ce.Sums[repo]; fresh && hit {
			savedTokens += sum
			continue
		}

		sum := sumMeterFile(path, repo)
		if ce.Sums == nil {
			ce.Sums = map[string]int64{}
		}
		ce.Sums[repo] = sum
		cache.Entries[path] = ce
		savedTokens += sum
		dirty = true
	}

	if dirty {
		saveMeterCache(cachePath, cache)
	}
	return savedTokens
}

// sumMeterFile returns the sum of tokens_saved_est across entries in path
// whose `repo` field equals root or has root+"/" as a prefix (sub-repos).
func sumMeterFile(path, root string) int64 {
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
		if e.Repo != root && !strings.HasPrefix(e.Repo, root+"/") {
			continue
		}
		total += e.TokensSavedEst
	}
	return total
}

// RepoOutputTokens returns the sum of assistant output tokens across all
// Claude Code transcripts for the given repo root path.
// projectsDir defaults to ~/.claude/projects if empty.
func RepoOutputTokens(projectsDir, repoRoot string) (int64, error) {
	if projectsDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, err
		}
		projectsDir = filepath.Join(home, ".claude", "projects")
	}
	repo := meter.RepoKey(repoRoot)
	_, _, _, outTokens := readAllTranscripts(projectsDir, repo)
	return outTokens, nil
}

// RepoCacheMix returns the fresh-input, cache-creation, and cache-read token
// counts summed across all Claude Code transcripts for the given repo root.
// It exposes the same telemetry compute() uses so callers outside this package
// (e.g. internal/report) can price saved input tokens at the identical
// cache-aware blended rate via BlendedInputMultiplier.
// projectsDir defaults to ~/.claude/projects if empty.
func RepoCacheMix(projectsDir, repoRoot string) (in, cacheCreation, cacheRead int64, err error) {
	if projectsDir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return 0, 0, 0, herr
		}
		projectsDir = filepath.Join(home, ".claude", "projects")
	}
	repo := meter.RepoKey(repoRoot)
	in, cacheCreation, cacheRead, _ = readAllTranscripts(projectsDir, repo)
	return in, cacheCreation, cacheRead, nil
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

// ReadCWDFromTranscriptPath returns the session cwd by reading the transcript
// at path and, if not found there, scanning siblings in the same directory.
// Returns "" if path is empty or no cwd line is found.
func ReadCWDFromTranscriptPath(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	// Try the named file first via a single-file scan.
	if cwd := readCWDFromSingleFile(transcriptPath); cwd != "" {
		return cwd
	}
	// Fall back to scanning siblings.
	return readProjectCWD(filepath.Dir(transcriptPath))
}

// readCWDFromSingleFile scans one transcript file for the first cwd field.
func readCWDFromSingleFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)
	for sc.Scan() {
		var probe struct {
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &probe) == nil && probe.CWD != "" {
			return probe.CWD
		}
	}
	return ""
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
		if cwd := readCWDFromSingleFile(filepath.Join(dir, f.Name())); cwd != "" {
			return cwd
		}
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

	// Resolve cache path.
	home := OverrideHome
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	cachePath := filepath.Join(home, ".pakka", "transcript-cache.json")
	cache := loadTranscriptCache(cachePath)
	dirty := false

	cwdToRepo := make(map[string]string)
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
		resolved, ok := cwdToRepo[cwd]
		if !ok {
			resolved = resolveRepoKey(cwd)
			cwdToRepo[cwd] = resolved
		}
		if resolved != repo && !strings.HasPrefix(resolved, repo+"/") {
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
			absPath := filepath.Join(dirPath, f.Name())
			info, err := f.Info()
			if err != nil {
				continue
			}
			mtime := info.ModTime().UnixNano()
			size := info.Size()

			if entry, ok := cache.Entries[absPath]; ok && entry.Mtime == mtime && entry.Size == size {
				// Cache hit — use pre-summed values.
				in += entry.InTokens
				cacheCreation += entry.CacheCreation
				cacheRead += entry.CacheRead
				out += entry.OutTokens
				continue
			}

			// Cache miss — re-sum and update.
			ti, tcc, tcr, to := sumTranscriptFile(absPath)
			in += ti
			cacheCreation += tcc
			cacheRead += tcr
			out += to
			cache.Entries[absPath] = transcriptCacheEntry{
				Mtime: mtime, Size: size,
				InTokens: ti, CacheCreation: tcc, CacheRead: tcr, OutTokens: to,
			}
			dirty = true
		}
	}

	if dirty {
		saveTranscriptCache(cachePath, cache)
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
