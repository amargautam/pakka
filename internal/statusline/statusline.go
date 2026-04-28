// Package statusline prints a compact pakka session summary to stdout.
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
	"time"

	"github.com/amargautam/pakka/internal/hookevent"
)

// Uncalibrated multipliers for v0.1.0. Pass 5 bench replaces these with measured values.
var outputMultiplier = map[string]float64{
	"lite":   0.11,
	"strict": 0.33,
	"ultra":  0.67,
	"audit":  0.0,
}

// metrics holds computed status-line values.
type metrics struct {
	compressMode   string
	tokensSavedEst int64
	inTokens       int64 // input tokens used as inPct denominator base
	inTokensKnown  bool  // true if input tokens came from transcript or meter
	inPct          int64
	outTokens      int64 // assistant output tokens this session (0 if unknown)
	outTokensKnown bool  // true if we successfully read transcript or meter
	outSavedEst    int64
	outPct         int64
	bugsCaught     int
}

// pctRound returns round(num*100/denom) as int64. Returns 0 when denom <= 0.
// Uses math.Round so 0.4→0, 0.5→1, 24.6→25.
func pctRound(num, denom int64) int64 {
	if denom <= 0 {
		return 0
	}
	return int64(math.Round(float64(num) * 100 / float64(denom)))
}

// compute gathers all status-line metrics from disk.
func compute(event *hookevent.Event, compressMode string) metrics {
	sid := shortSID(event.SessionID)
	home, _ := os.UserHomeDir()

	// Read meter data (populated by Pass 2+).
	meterPath := filepath.Join(home, ".pakka", "meter", sid+".jsonl")
	meterTokensUsed, _, tokensSavedEst, meterOutputTokens := readMeter(meterPath)
	sessionStart := meterSessionStart(meterPath)

	if compressMode == "" {
		compressMode = "strict"
	}

	// Resolve input + output tokens from transcript (authoritative when present).
	// Fall back to meter values when transcript is missing/unparseable.
	var transcriptIn, transcriptOut int64
	transcriptOK := false
	if event.TranscriptPath != "" {
		if in, out, ok := readTranscript(event.TranscriptPath); ok {
			transcriptIn = in
			transcriptOut = out
			transcriptOK = true
		}
	}

	// Input tokens: transcript wins. Fall back to meter tokensUsed for sessions
	// without a transcript path. When neither has signal, inPct renders 0%.
	var inTokens int64
	inKnown := false
	if transcriptOK && transcriptIn > 0 {
		inTokens = transcriptIn
		inKnown = true
	} else if meterTokensUsed > 0 {
		inTokens = meterTokensUsed
		inKnown = true
	}

	inPct := pctRound(tokensSavedEst, inTokens+tokensSavedEst)

	// Output tokens: transcript wins. Fall back to meter output_tokens.
	var outTokens int64
	outKnown := false
	if transcriptOK && transcriptOut > 0 {
		outTokens = transcriptOut
		outKnown = true
	} else if meterOutputTokens > 0 {
		outTokens = meterOutputTokens
		outKnown = true
	}

	mult := outputMultiplier[compressMode]
	outSaved := int64(float64(outTokens) * mult)

	// Output savings percentage (integer-rounded). When unmeasured this is 0.
	outPct := pctRound(outSaved, outTokens+outSaved)

	// Count bugs caught from review findings (scoped to current session).
	cwd := event.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	bugs := countBugsCaught(filepath.Join(cwd, ".pakka", "reviews"), sessionStart)

	return metrics{
		compressMode:   compressMode,
		tokensSavedEst: tokensSavedEst,
		inTokens:       inTokens,
		inTokensKnown:  inKnown,
		inPct:          inPct,
		outTokens:      outTokens,
		outTokensKnown: outKnown,
		outSavedEst:    outSaved,
		outPct:         outPct,
		bugsCaught:     bugs,
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

// formatLine renders the status-line body using the supplied glyphs.
// inArrow/outArrow are the prefix tokens; sep is the separator between sections.
//
// New format (Pass 4.x): show only integer-rounded percentages — no raw counts,
// no `--` placeholder, no `[meas]/[est]` labels. When output is unmeasured the
// outPct is 0, which renders identically to a measured-but-rounds-down 0%.
func formatLine(m metrics, inArrow, outArrow, sep string) string {
	return fmt.Sprintf("[%s] %s %s%d%% / %s%d%% tok saved %s %d bugs caught",
		m.compressMode, sep, inArrow, m.inPct, outArrow, m.outPct, sep, m.bugsCaught)
}

// Run prints the pakka status line to w.
//
// Format (UTF-8): pakka [strict] · ↑0% / ↓25% tok saved · 0 bugs caught
// Format (ascii): pakka [strict] | in 0% / out 25% tok saved | 0 bugs caught
//
// Arrows follow conventional download/upload semantics from the user's
// perspective: ↑ = prompt going UP to the API (input), ↓ = response coming
// DOWN (output).
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
// Only files modified at or after since are counted.
func countBugsCaught(dir string, since time.Time) int {
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

		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(since) {
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

// meterEntry matches the JSONL written by the meter subcommand (Pass 2+).
type meterEntry struct {
	TokensUsed     int64 `json:"tokens_used"`
	BytesSaved     int64 `json:"bytes_saved"`
	TokensSavedEst int64 `json:"tokens_saved_est"`
	OutputTokens   int64 `json:"output_tokens"`
}

// readMeter reads meter JSONL and sums usage, savings, and output tokens.
// Returns (0, 0, 0, 0) if file doesn't exist or is unreadable.
func readMeter(path string) (used, bytesSaved, tokensSavedEst, outputTokens int64) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Allow longer lines than the 64KB default — transcript-derived entries
	// are tiny but defensive sizing is cheap.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		var e meterEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			used += e.TokensUsed
			bytesSaved += e.BytesSaved
			tokensSavedEst += e.TokensSavedEst
			outputTokens += e.OutputTokens
		}
	}
	return used, bytesSaved, tokensSavedEst, outputTokens
}

// readTranscript opens a Claude Code transcript JSONL and sums input + output
// tokens across assistant turns.
//
// Input is the sum of usage.input_tokens + usage.cache_read_input_tokens +
// usage.cache_creation_input_tokens. Output is usage.output_tokens.
//
// Returns (inTokens, outTokens, true) when at least one line yielded a non-zero
// in OR out total; (0, 0, false) otherwise (file missing, unreadable, or all
// usage fields zero).
//
// Tolerates missing fields — transcript schemas vary across Claude Code
// versions. Tries two candidate JSON shapes.
func readTranscript(path string) (int64, int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)

	var inTotal, outTotal int64
	parsed := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// Candidate shape A: {"message":{"usage":{...}}}
		var a struct {
			Message struct {
				Usage struct {
					InputTokens              int64 `json:"input_tokens"`
					OutputTokens             int64 `json:"output_tokens"`
					CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
					CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &a) == nil {
			u := a.Message.Usage
			lineIn := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
			if lineIn > 0 || u.OutputTokens > 0 {
				inTotal += lineIn
				outTotal += u.OutputTokens
				parsed = true
				continue
			}
		}
		// Candidate shape B: {"usage":{...}}
		var b struct {
			Usage struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &b) == nil {
			u := b.Usage
			lineIn := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
			if lineIn > 0 || u.OutputTokens > 0 {
				inTotal += lineIn
				outTotal += u.OutputTokens
				parsed = true
				continue
			}
		}
	}
	if !parsed {
		return 0, 0, false
	}
	return inTotal, outTotal, true
}

// meterSessionStart returns the timestamp of the first meter entry for the
// current session. This is used to scope bug counts to the current session
// rather than counting stale findings from previous sessions.
func meterSessionStart(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return time.Time{}
	}
	var entry struct {
		TS string `json:"ts"`
	}
	if json.Unmarshal(sc.Bytes(), &entry) != nil {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, entry.TS); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, entry.TS); err == nil {
		return t
	}
	return time.Time{}
}
