package statusline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/hookevent"
)

// useFakeHome redirects HOME and the OverrideHome hook so all status-line
// reads come from the supplied directory. Restores both on test cleanup.
func useFakeHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	prev := OverrideHome
	OverrideHome = home
	t.Cleanup(func() { OverrideHome = prev })
}

// useFakeRepoKey replaces the package's RepoKey resolver with a fixed map.
// Pass nil to fall back to identity (cwd → cwd).
func useFakeRepoKey(t *testing.T, mapping map[string]string) {
	t.Helper()
	prev := OverrideRepoKey
	OverrideRepoKey = func(cwd string) string {
		if v, ok := mapping[cwd]; ok {
			return v
		}
		return cwd
	}
	t.Cleanup(func() { OverrideRepoKey = prev })
}

// writeMeterEntry appends one JSONL line to a meter file under home.
func writeMeterEntry(t *testing.T, home, sid string, entry map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".pakka", "meter")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(entry)
	f, err := os.OpenFile(filepath.Join(dir, sid+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

// writeTranscriptDir creates a fake project directory under
// ~/.claude/projects/<encoded>/ and writes one transcript JSONL with the
// supplied per-turn usage maps.
func writeTranscriptDir(t *testing.T, home, encodedName, transcriptName string, turns []map[string]int64) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", encodedName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, u := range turns {
		line := map[string]any{
			"type":    "assistant",
			"message": map[string]any{"usage": u},
		}
		data, _ := json.Marshal(line)
		sb.Write(data)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, transcriptName), []byte(sb.String()), 0600); err != nil {
		t.Fatal(err)
	}
}

// run invokes Run and returns rendered string.
func run(t *testing.T, event *hookevent.Event, level string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Run(event, &buf, level); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// extractInPct parses the percent from the input ("↑<abs> (<pct>%)") segment.
func extractInPct(s string) int { return extractPctAfter(s, "↑") }

// extractPctAfter parses the percent from "<marker><abs> (<pct>%)" form.
// Looks for the first "(" after the marker and reads digits up to "%".
func extractPctAfter(s, marker string) int {
	i := strings.Index(s, marker)
	if i < 0 {
		return -1
	}
	rest := s[i+len(marker):]
	openIdx := strings.Index(rest, "(")
	if openIdx < 0 {
		return -1
	}
	rest = rest[openIdx+1:]
	pctIdx := strings.Index(rest, "%")
	if pctIdx <= 0 {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(rest[:pctIdx], "%d", &n); err != nil {
		return -1
	}
	return n
}

// extractInAbs parses the humanized absolute saved-token string after "↑".
// Returns the literal token like "0", "847", "12.4K", "1.2M".
func extractInAbs(s string) string  { return extractAbsAfter(s, "↑") }
func extractOutAbs(s string) string { return extractAbsAfter(s, "↓") }

func extractAbsAfter(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i+len(marker):]
	spIdx := strings.Index(rest, " ")
	if spIdx <= 0 {
		return ""
	}
	return rest[:spIdx]
}

// extractBugs parses "<N> bugs caught" out of the rendered string.
func extractBugs(s string) int {
	idx := strings.LastIndex(s, " bugs caught")
	if idx < 0 {
		return -1
	}
	prefix := s[:idx]
	spIdx := strings.LastIndex(prefix, " ")
	if spIdx < 0 {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(prefix[spIdx+1:], "%d", &n); err != nil {
		return -1
	}
	return n
}

// stripBracketLabel removes "[<level>]" once from the rendered line so two
// renders that differ only in their bracket label compare equal.
var bracketRe = regexp.MustCompile(`\[(lite|strict|ultra|super-ultra)\]`)

func stripBracketLabel(s string) string {
	return bracketRe.ReplaceAllString(s, "[LEVEL]")
}

// --- core format / arrows / fallback ---

func TestRunOutputBaseFormat(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	out := run(t, &hookevent.Event{SessionID: "abc12345xyz", CWD: "/work/x"}, "strict")

	if len(out) >= 200 {
		t.Errorf("output too long (%d): %q", len(out), out)
	}
	for _, want := range []string{"pakka", "in saved", "out tok", "bugs caught", "[strict]", "↑", "↓"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	// Empty-fixture run: input shows zero+0%, output shows zero volume.
	if !strings.Contains(out, "↑0 (0%)") {
		t.Errorf("missing input zero+pct in %q", out)
	}
	if !strings.Contains(out, "↓0 ") {
		t.Errorf("missing output zero in %q", out)
	}
	// "--" placeholder must never appear.
	for _, bad := range []string{"(--)", "--"} {
		if strings.Contains(out, bad) {
			t.Errorf("banned %q in %q", bad, out)
		}
	}
}

// TestNoOutputPercent — regression guard. The output side must not render a
// `%` after the down-arrow segment. Captures the Pass 4.1 disease where a
// placeholder multiplier was displayed as if measured.
func TestNoOutputPercent(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})
	// Force non-zero output volume so any latent % code path would trigger.
	writeTranscriptDir(t, t.TempDir(), "-repo-A", "t.jsonl", nil) // safe noop in fresh tempdir
	home := t.TempDir()
	useFakeHome(t, home)
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 5000},
	})
	for _, level := range []string{"lite", "strict", "ultra"} {
		out := run(t, &hookevent.Event{SessionID: "noPctTst", CWD: "/repo/A"}, level)
		// After "↓<abs>" there must be NO "(...)%" — only " out tok".
		downIdx := strings.Index(out, "↓")
		if downIdx < 0 {
			t.Fatalf("missing down-arrow at level=%s: %q", level, out)
		}
		tail := out[downIdx:]
		// First space-token after "↓<abs>" must be "out", not "(...)".
		fields := strings.Fields(tail)
		if len(fields) < 2 {
			t.Fatalf("unexpected tail at level=%s: %q", level, tail)
		}
		// fields[0] is "↓<abs>", fields[1] should be "out".
		if fields[1] != "out" {
			t.Errorf("level=%s: expected next token after ↓abs to be 'out', got %q (line=%q)",
				level, fields[1], out)
		}
		// Hard guard: the substring "%) " or " %)" must not appear after the down-arrow.
		if strings.Contains(tail, "%") {
			t.Errorf("level=%s: output side contains a %% (theatre regression): %q", level, out)
		}
	}
}

// TestOutputLevelLabelVaries — table-driven across {lite, strict, ultra}.
// Same fixture; only the bracket label changes. Once labels are stripped, the
// rendered string is byte-identical across levels — proves outputLevel does
// not silently drive any displayed metric.
func TestOutputLevelLabelVaries(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})

	writeMeterEntry(t, home, "lvltest1", map[string]any{
		"ts": "t", "session_id": "lvltest1", "repo": "/repo/A",
		"tokens_saved_est": 200,
	})
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 1800, "output_tokens": 4000},
	})

	rendered := map[string]string{}
	for _, level := range []string{"lite", "strict", "ultra"} {
		out := run(t, &hookevent.Event{SessionID: "lvltest1", CWD: "/repo/A"}, level)
		rendered[level] = out

		// Bracket carries the level.
		if !strings.Contains(out, "["+level+"]") {
			t.Errorf("level=%s: bracket label missing in %q", level, out)
		}
		// Same input absolute across all levels (sanity).
		if g := extractInAbs(out); g != "200" {
			t.Errorf("level=%s: inAbs want 200, got %q", level, g)
		}
		// Output absolute must be identical too (we no longer multiply for display).
		if g := extractOutAbs(out); g != "4.0K" {
			t.Errorf("level=%s: outAbs want 4.0K, got %q", level, g)
		}
	}

	// Behavior that varies: only the bracket label.
	liteStripped := stripBracketLabel(rendered["lite"])
	strictStripped := stripBracketLabel(rendered["strict"])
	ultraStripped := stripBracketLabel(rendered["ultra"])
	if liteStripped != strictStripped {
		t.Errorf("lite vs strict differ beyond bracket:\n lite=%q\n strict=%q", liteStripped, strictStripped)
	}
	if strictStripped != ultraStripped {
		t.Errorf("strict vs ultra differ beyond bracket:\n strict=%q\n ultra=%q", strictStripped, ultraStripped)
	}
	// And the unstripped forms differ from each other (the bracket itself).
	if rendered["lite"] == rendered["strict"] || rendered["strict"] == rendered["ultra"] {
		t.Errorf("levels render identically: %q / %q / %q",
			rendered["lite"], rendered["strict"], rendered["ultra"])
	}
}

func TestArrowDirectionConventional(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	out := run(t, &hookevent.Event{SessionID: "arrow001", CWD: "/r"}, "strict")

	upIdx := strings.Index(out, "↑")
	downIdx := strings.Index(out, "↓")
	if !(upIdx > 0 && upIdx < downIdx) {
		t.Errorf("expected ↑ before ↓: %q (idx %d/%d)", out, upIdx, downIdx)
	}
}

func TestRunAsciiFallback(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "C")
	t.Setenv("LC_CTYPE", "")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)

	out := run(t, &hookevent.Event{SessionID: "ascii123", CWD: "/r"}, "strict")
	if strings.Contains(out, "↓") || strings.Contains(out, "↑") {
		t.Errorf("expected ascii: %q", out)
	}
	for _, want := range []string{"in 0 (0%)", "out 0 ", "saved", "tok"} {
		if !strings.Contains(out, want) {
			t.Errorf("ascii missing %q in %q", want, out)
		}
	}
	// No '%' after the "out " token.
	if outIdx := strings.Index(out, "out "); outIdx > 0 {
		tail := out[outIdx:]
		if strings.Contains(tail, "%") {
			t.Errorf("ascii output side must not contain %%: %q", out)
		}
	}
}

func TestRunDefaultLevel(t *testing.T) {
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	out := run(t, &hookevent.Event{SessionID: "test1234", CWD: "/r"}, "")
	if !strings.Contains(out, "[strict]") {
		t.Errorf("default level should be [strict]: %q", out)
	}
}

// --- behavioral aggregation tests ---

// Cross-session aggregation: 3 meter files for same repo, savings 100/200/300.
// Combined inSavedTokens=600 → with no transcripts denom=600, pct=100%.
// Then add transcript input to confirm the SUM (not just one file) drives denom.
func TestCrossSessionAggregation(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})

	const repo = "/repo/A"
	for sid, saved := range map[string]int64{"sess0001": 100, "sess0002": 200, "sess0003": 300} {
		writeMeterEntry(t, home, sid, map[string]any{
			"ts": "2025-01-01T00:00:00Z", "session_id": sid, "repo": repo,
			"tokens_saved_est": saved,
		})
	}

	out := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	if extractInPct(out) != 100 {
		t.Errorf("aggregated inPct want 100, got %q", out)
	}

	// Add transcript input=900; denom = 900 + 600 saved = 1500; pct = round(600/1500*100) = 40.
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 900, "output_tokens": 0},
	})
	out2 := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	pct2 := extractInPct(out2)
	if pct2 != 40 {
		t.Errorf("after adding transcript inputs, want 40 got %d (out=%q)", pct2, out2)
	}
	if !(pct2 < 100) {
		t.Errorf("inPct should drop with transcript input; got %d", pct2)
	}
}

// Repo isolation: foreign-repo and legacy (no-repo) meter entries excluded.
func TestRepoIsolation(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})

	writeMeterEntry(t, home, "ownsess1", map[string]any{
		"ts": "t", "session_id": "ownsess1", "repo": "/repo/A",
		"tokens_saved_est": 500,
	})
	writeMeterEntry(t, home, "fornses1", map[string]any{
		"ts": "t", "session_id": "fornses1", "repo": "/repo/B",
		"tokens_saved_est": 9999,
	})
	writeMeterEntry(t, home, "leg00001", map[string]any{
		"ts": "t", "session_id": "leg00001",
		"tokens_saved_est": 9999,
	})

	out := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	// Only own session counted: denom = 500 → pct = 100%.
	if extractInPct(out) != 100 {
		t.Errorf("isolation broken; got %q", out)
	}
}

// Cost weights applied: input=200, cache_creation=400, cache_read=1000, savedTokens=50.
// denom = 200 + 1.25×400 + 0.1×1000 + 50 = 850. pct = round(50/850*100) = 6.
func TestCostWeightsApplied(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})

	writeMeterEntry(t, home, "savsess1", map[string]any{
		"ts": "t", "session_id": "savsess1", "repo": "/repo/A",
		"tokens_saved_est": 50,
	})
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 200, "cache_creation_input_tokens": 400, "cache_read_input_tokens": 1000, "output_tokens": 0},
	})

	out := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	if pct := extractInPct(out); pct != 6 {
		t.Errorf("cost-weighted pct want 6, got %d (out=%q)", pct, out)
	}
}

// Cache_read does NOT collapse pct to 0: 10× cache_read with same setup as
// the baseline. denom = 200 + 500 + 1000 + 50 = 1750. pct = round(50/1750*100) = 3.
// Behavioral: 3 < 6 (less per-token contribution than baseline) but non-zero.
func TestCacheReadDoesNotCollapseRatio(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})

	writeMeterEntry(t, home, "savsess1", map[string]any{
		"ts": "t", "session_id": "savsess1", "repo": "/repo/A",
		"tokens_saved_est": 50,
	})
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 200, "cache_creation_input_tokens": 400, "cache_read_input_tokens": 10000, "output_tokens": 0},
	})

	out := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	pct := extractInPct(out)
	if pct != 3 {
		t.Errorf("cache-read-heavy pct want 3, got %d (out=%q)", pct, out)
	}
	if pct == 0 {
		t.Errorf("cache_read must not collapse pct to 0")
	}
	if !(pct < 6) {
		t.Errorf("more cache_read should yield smaller pct than baseline 6: got %d", pct)
	}
}

// Output cumulative: 2 transcripts in same project dir. output 1000+2000 = 3000.
// Display shows the absolute volume (humanized) — 3000 → "3.0K" — and is
// identical across levels (no per-level multiplier in the displayed value).
func TestOutputCumulative(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})

	writeTranscriptDir(t, home, "-repo-A", "t1.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 1000},
	})
	writeTranscriptDir(t, home, "-repo-A", "t2.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 2000},
	})

	out := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	if g := extractOutAbs(out); g != "3.0K" {
		t.Errorf("cumulative outAbs want 3.0K, got %q", g)
	}

	// Output absolute must NOT change with level (the post-display multiplier
	// is gone).
	outU := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "ultra")
	if extractOutAbs(outU) != extractOutAbs(out) {
		t.Errorf("outAbs must be invariant across levels: strict=%q ultra=%q",
			extractOutAbs(out), extractOutAbs(outU))
	}
}

// Bugs all-time: every entry counted regardless of file mod time. Verdict
// files excluded.
func TestBugsAllTimeNoTimeFilter(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)

	repo := t.TempDir()
	useFakeRepoKey(t, map[string]string{"/work/A": repo})

	dir := filepath.Join(repo, ".pakka", "reviews")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	mk := func(name string, lines []string) {
		if err := os.WriteFile(filepath.Join(dir, name),
			[]byte(strings.Join(lines, "\n")), 0600); err != nil {
			t.Fatal(err)
		}
	}
	hi := `{"severity":"error","confidence":90}`
	lo := `{"severity":"error","confidence":50}`
	warn := `{"severity":"warn","confidence":99}`

	mk("a.jsonl", []string{hi, lo, warn}) // 1 hi
	mk("b.jsonl", []string{hi})           // 1 hi
	mk("c.jsonl", []string{hi, hi})       // 2 hi
	mk("verdict-001.jsonl", []string{hi, hi, hi}) // ignored

	// Backdate `a.jsonl` to 2020 to confirm no time filter applies.
	old := filepath.Join(dir, "a.jsonl")
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	out := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/work/A"}, "strict")
	bugs := extractBugs(out)
	if bugs != 4 {
		t.Errorf("bugs all-time want 4 (1+1+2), got %d (out=%q)", bugs, out)
	}

	// Behavioral: adding more findings must increase the count.
	mk("d.jsonl", []string{hi, hi, hi})
	out2 := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/work/A"}, "strict")
	bugs2 := extractBugs(out2)
	if !(bugs2 > bugs) {
		t.Errorf("bugs should grow when more findings added: %d → %d", bugs, bugs2)
	}
	if bugs2 != 7 {
		t.Errorf("after add, want 7, got %d", bugs2)
	}
}

// --- helpers / unit checks ---

// TestHumanize covers the formatter directly. Floor truncation is
// intentional — verify the 999→"999" / 1000→"1.0K" boundary.
func TestHumanize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},        // boundary: still raw
		{1000, "1.0K"},      // boundary: enters K
		{1099, "1.0K"},      // floor: doesn't bump to 1.1K
		{1100, "1.1K"},
		{12450, "12.4K"},    // explicit spec example
		{999_999, "999.9K"}, // boundary: just below M
		{1_000_000, "1.0M"}, // boundary: enters M
		{1_234_567, "1.2M"}, // explicit spec example
		{12_500_000, "12.5M"},
		{-5, "0"}, // negative clamped
	}
	for _, tt := range tests {
		got := humanize(tt.in)
		if got != tt.want {
			t.Errorf("humanize(%d)=%q want %q", tt.in, got, tt.want)
		}
	}
}

// TestStatusLineShowsBothAbsoluteAndPercent — explicit guard against the
// percent-only regression on the input side. For two distinct inSavedTokens
// fixtures, the rendered string must contain BOTH humanized absolute counts.
func TestStatusLineShowsBothAbsoluteAndPercent(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")

	render := func(saved int64) string {
		home := t.TempDir()
		useFakeHome(t, home)
		useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})
		writeMeterEntry(t, home, "varies01", map[string]any{
			"ts": "t", "session_id": "varies01", "repo": "/repo/A",
			"tokens_saved_est": saved,
		})
		// Add transcript input so denom > saved.
		writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
			{"input_tokens": saved * 9, "output_tokens": 0}, // pct ≈ 10
		})
		return run(t, &hookevent.Event{SessionID: "varies01", CWD: "/repo/A"}, "strict")
	}

	// Two distinct inputs → two distinct humanized values.
	smallOut := render(800)    // < 1000 → "800"
	largeOut := render(15_000) // → "15.0K"

	if !strings.Contains(smallOut, "↑800 ") {
		t.Errorf("small render missing absolute '↑800 ': %q", smallOut)
	}
	if !strings.Contains(largeOut, "↑15.0K ") {
		t.Errorf("large render missing absolute '↑15.0K ': %q", largeOut)
	}
	// Critical: small absolute must NOT appear in large render and vice versa.
	if strings.Contains(largeOut, "↑800 ") {
		t.Errorf("large render leaked small abs token: %q", largeOut)
	}
	if strings.Contains(smallOut, "↑15.0K") {
		t.Errorf("small render leaked large abs token: %q", smallOut)
	}
	// Both must still carry the input-side parenthesized percent.
	for _, s := range []string{smallOut, largeOut} {
		if !strings.Contains(s, "%)") {
			t.Errorf("missing parenthesized percent: %q", s)
		}
	}
	if extractInAbs(smallOut) == extractInAbs(largeOut) {
		t.Errorf("inAbs must vary across inputs: small=%q large=%q",
			extractInAbs(smallOut), extractInAbs(largeOut))
	}
}

// TestExtractPctFormat confirms the input-side extractor handles "↑<abs> (<pct>%)".
func TestExtractPctFormat(t *testing.T) {
	cases := []struct {
		s       string
		inWant  int
		inAbsW  string
		outAbsW string
	}{
		{"x ↑0 (0%) in saved · ↓0 out tok y", 0, "0", "0"},
		{"x ↑5K (5%) in saved · ↓1K out tok y", 5, "5K", "1K"},
		{"x ↑12.4K (43%) in saved · ↓7.0K out tok y", 43, "12.4K", "7.0K"},
		{"x ↑847 (88%) in saved · ↓100 out tok y", 88, "847", "100"},
	}
	for _, c := range cases {
		if g := extractInPct(c.s); g != c.inWant {
			t.Errorf("extractInPct(%q)=%d want %d", c.s, g, c.inWant)
		}
		if g := extractInAbs(c.s); g != c.inAbsW {
			t.Errorf("extractInAbs(%q)=%q want %q", c.s, g, c.inAbsW)
		}
		if g := extractOutAbs(c.s); g != c.outAbsW {
			t.Errorf("extractOutAbs(%q)=%q want %q", c.s, g, c.outAbsW)
		}
	}
}

func TestPctRound(t *testing.T) {
	tests := []struct {
		num, denom int64
		want       int64
	}{
		{0, 0, 0}, {5, 0, 0}, {0, 100, 0},
		{4, 1000, 0}, {6, 1000, 1}, {246, 1000, 25},
		{500, 1000, 50}, {999, 1000, 100}, {1000, 1000, 100},
	}
	for _, tt := range tests {
		got := pctRound(tt.num, tt.denom)
		if got != tt.want {
			t.Errorf("pctRound(%d,%d)=%d want %d", tt.num, tt.denom, got, tt.want)
		}
	}
	const denom int64 = 1000
	prev := int64(-1)
	for n := int64(0); n <= 1000; n += 100 {
		g := pctRound(n, denom)
		if g < prev {
			t.Errorf("non-monotonic: n=%d got=%d prev=%d", n, g, prev)
		}
		prev = g
	}
}

// readAllTranscripts: behavioral — output VARIES with transcript input.
func TestReadAllTranscriptsVaries(t *testing.T) {
	for _, n := range []int64{100, 500, 2000} {
		home := t.TempDir()
		useFakeHome(t, home)
		useFakeRepoKey(t, map[string]string{"/r": "/r"})
		writeTranscriptDir(t, home, "-r", "t.jsonl", []map[string]int64{
			{"input_tokens": n, "output_tokens": 1},
		})
		in, _, _, _ := readAllTranscripts(filepath.Join(home, ".claude", "projects"), "/r")
		if in != n {
			t.Errorf("inTokens=%d want %d", in, n)
		}
	}
}

func TestDecodeProjectDir(t *testing.T) {
	cases := []struct{ enc, dec string }{
		{"-repo-A", "/repo/A"},
		{"-Users-amar-Projects-pakka-dev", "/Users/amar/Projects/pakka/dev"},
		{"", ""},
	}
	for _, c := range cases {
		got := decodeProjectDir(c.enc)
		if got != c.dec {
			t.Errorf("decodeProjectDir(%q)=%q want %q", c.enc, got, c.dec)
		}
	}
}

func TestShortSID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abc12345xyz", "abc12345"},
		{"short", "short"},
		{"exactly8", "exactly8"},
		{"", ""},
	}
	for _, tt := range tests {
		got := shortSID(tt.in)
		if got != tt.want {
			t.Errorf("shortSID(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestUtf8Capable(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_CTYPE", "")
	if !utf8Capable() {
		t.Error("UTF-8 LANG should be detected")
	}
	t.Setenv("LANG", "C")
	if utf8Capable() {
		t.Error("LANG=C should NOT be utf8")
	}
	t.Setenv("LC_ALL", "en_US.utf8")
	if !utf8Capable() {
		t.Error("LC_ALL with utf8 should be detected")
	}
}

func TestSummaryNoANSI(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	got := Summary(&hookevent.Event{SessionID: "sum12345", CWD: "/r"}, "strict")
	if strings.Contains(got, "\033[") {
		t.Errorf("Summary should not contain ANSI escapes: %q", got)
	}
	for _, want := range []string{"in saved", "out tok", "↑0 (0%)", "↓0 "} {
		if !strings.Contains(got, want) {
			t.Errorf("Summary missing %q: %q", want, got)
		}
	}
}

// TestStaleCompressGlyph — when orchestrator state has failed entries, the
// status-line MUST append "! N stale". Empty/missing state MUST omit it.
func TestStaleCompressGlyph(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	repoDir := t.TempDir()
	useFakeRepoKey(t, map[string]string{"/work/X": repoDir})

	// Case 1: missing state file → no glyph.
	out := run(t, &hookevent.Event{SessionID: "stale001", CWD: "/work/X"}, "strict")
	if strings.Contains(out, "stale") {
		t.Errorf("missing state must not render stale glyph: %q", out)
	}

	// Case 2: state file with 2 failures + 1 success → "! 2 stale".
	stateDir := filepath.Join(repoDir, ".pakka")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "/x/CLAUDE.md": {"sourceSHA":"a","level":"strict","compressedAt":"t","validatorPasses":false},
  "/x/DESIGN.md": {"sourceSHA":"b","level":"strict","compressedAt":"t","validatorPasses":false},
  "/x/BUILD.md":  {"sourceSHA":"c","level":"strict","compressedAt":"t","validatorPasses":true}
}`
	if err := os.WriteFile(filepath.Join(stateDir, "compress-state.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out2 := run(t, &hookevent.Event{SessionID: "stale002", CWD: "/work/X"}, "strict")
	if !strings.Contains(out2, "! 2 stale") {
		t.Errorf("expected '! 2 stale' segment: %q", out2)
	}

	// Case 3: corrupt state → graceful zero, no glyph.
	if err := os.WriteFile(filepath.Join(stateDir, "compress-state.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	out3 := run(t, &hookevent.Event{SessionID: "stale003", CWD: "/work/X"}, "strict")
	if strings.Contains(out3, "stale") {
		t.Errorf("corrupt state must not render stale glyph: %q", out3)
	}
}

// TestUnknownLevelDefaultsToStrict — invalid levels (e.g. legacy "audit")
// must render as [strict] and not crash on a missing multiplier key.
func TestUnknownLevelDefaultsToStrict(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	for _, bad := range []string{"audit", "fast", "garbage", "Strict"} {
		out := run(t, &hookevent.Event{SessionID: "bad12345", CWD: "/r"}, bad)
		if !strings.Contains(out, "[strict]") {
			t.Errorf("level=%q: expected fallback to [strict]: %q", bad, out)
		}
	}
}
