package statusline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// run invokes Run and returns rendered string. stale is the pre-computed
// orchestrator stale count (0 in most tests).
func run(t *testing.T, event *hookevent.Event, level string, stale ...int) string {
	t.Helper()
	s := 0
	if len(stale) > 0 {
		s = stale[0]
	}
	var buf bytes.Buffer
	if err := Run(event, &buf, level, s); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// summary invokes Summary and returns the plain-text full-format string.
// Behavioral/math tests use this so metric assertions survive the Run() trim.
// stale is the pre-computed orchestrator stale count (0 in most tests).
func summary(t *testing.T, event *hookevent.Event, level string, stale ...int) string {
	t.Helper()
	s := 0
	if len(stale) > 0 {
		s = stale[0]
	}
	return Summary(event, level, s)
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

// --- core format / arrows / fallback ---

func TestRunOutputBaseFormat(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	out := run(t, &hookevent.Event{SessionID: "abc12345xyz", CWD: "/work/x"}, "strict")

	// Run() now emits only "pakka [<level>]" — verify shape.
	if len(out) >= 200 {
		t.Errorf("output too long (%d): %q", len(out), out)
	}
	for _, want := range []string{"pakka", "[strict]"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	// Trimmed: token/bug segments must not appear in Run() output.
	for _, gone := range []string{"tokens saved", "bugs caught", "↑", "↓", "in saved", "out tok"} {
		if strings.Contains(out, gone) {
			t.Errorf("removed segment %q must not appear in Run() output: %q", gone, out)
		}
	}
}

// TestOutputPercentRendered — output side renders Y% derived from the
// level's outputMultiplier (round(mult/(1+mult)*100)). Y% is a placeholder
// constant per level until v0.2.0 baseline-vs-compressed bench measures
// the real ratio (see outputMultiplier doc).
func TestOutputPercentRendered(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})
	// Force non-zero output volume so the rendered tail is realistic.
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 5000},
	})
	expected := map[string]int{
		"lite":        10,
		"strict":      25,
		"ultra":       40,
		"super-ultra": 44,
	}
	for level, want := range expected {
		out := summary(t, &hookevent.Event{SessionID: "outPctTst", CWD: "/repo/A"}, level)
		downIdx := strings.Index(out, "↓")
		if downIdx < 0 {
			t.Fatalf("missing down-arrow at level=%s: %q", level, out)
		}
		got := extractPctAfter(out, "↓")
		if got != want {
			t.Errorf("level=%s: outPct want %d, got %d (line=%q)", level, want, got, out)
		}
	}
}

// TestOutputPercentVariesByLevel — behavioral guard per memory feedback
// "tests must verify metrics VARY with input". Same input/output token
// volume across all four levels must yield four DIFFERENT Y% values. If a
// future regression collapses Y% to a constant across levels, this test
// fails loudly.
func TestOutputPercentVariesByLevel(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 1000, "output_tokens": 5000},
	})

	seen := map[int]string{}
	for _, level := range []string{"lite", "strict", "ultra", "super-ultra"} {
		out := summary(t, &hookevent.Event{SessionID: "varyTst1", CWD: "/repo/A"}, level)
		pct := extractPctAfter(out, "↓")
		if pct < 0 {
			t.Fatalf("level=%s: failed to extract outPct from %q", level, out)
		}
		if prior, ok := seen[pct]; ok {
			t.Errorf("outPct collapsed across levels: %s and %s both rendered %d%%",
				prior, level, pct)
		}
		seen[pct] = level
	}
	if len(seen) != 4 {
		t.Errorf("expected 4 distinct outPct values across levels, got %d: %v",
			len(seen), seen)
	}
}

// TestOutputAbsInvariantAcrossLevels — input savings absolute and output
// volume absolute must NOT vary with level (they are measured/observed,
// not level-derived). Only Y% on the output side varies with level — that
// behavior is covered separately in TestOutputPercentVariesByLevel.
func TestOutputAbsInvariantAcrossLevels(t *testing.T) {
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

	for _, level := range []string{"lite", "strict", "ultra", "super-ultra"} {
		out := summary(t, &hookevent.Event{SessionID: "lvltest1", CWD: "/repo/A"}, level)

		if !strings.Contains(out, "["+level+"]") {
			t.Errorf("level=%s: bracket label missing in %q", level, out)
		}
		if g := extractInAbs(out); g != "200" {
			t.Errorf("level=%s: inAbs want 200, got %q", level, g)
		}
		if g := extractOutAbs(out); g != "4.0K" {
			t.Errorf("level=%s: outAbs want 4.0K, got %q", level, g)
		}
	}
}

func TestArrowDirectionConventional(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)

	// Run() no longer emits arrows — verify arrow order via Summary().
	got := summary(t, &hookevent.Event{SessionID: "arrow001", CWD: "/r"}, "strict")
	upIdx := strings.Index(got, "↑")
	downIdx := strings.Index(got, "↓")
	if !(upIdx > 0 && upIdx < downIdx) {
		t.Errorf("expected ↑ before ↓ in Summary: %q (idx %d/%d)", got, upIdx, downIdx)
	}
}

func TestRunAsciiFallback(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "C")
	t.Setenv("LC_CTYPE", "")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)

	// Run() now emits only "pakka [<level>]" — locale no longer affects Run() output.
	out := run(t, &hookevent.Event{SessionID: "ascii123", CWD: "/r"}, "strict")
	for _, want := range []string{"pakka", "[strict]"} {
		if !strings.Contains(out, want) {
			t.Errorf("ascii: missing %q in %q", want, out)
		}
	}
	// No token/bug segments in trimmed Run() output.
	for _, gone := range []string{"tokens saved", "bugs caught", "in saved", "out tok"} {
		if strings.Contains(out, gone) {
			t.Errorf("ascii: removed segment %q must not appear: %q", gone, out)
		}
	}
	// ASCII locale verification lives in Summary — confirm Summary still uses ascii glyphs.
	got := summary(t, &hookevent.Event{SessionID: "ascii123", CWD: "/r"}, "strict")
	if strings.Contains(got, "↓") || strings.Contains(got, "↑") {
		t.Errorf("Summary in ascii locale should not emit UTF-8 arrows: %q", got)
	}
	for _, want := range []string{"in 0 (0%)", "out 0 (25%)", "tokens saved"} {
		if !strings.Contains(got, want) {
			t.Errorf("Summary ascii missing %q in %q", want, got)
		}
	}
}

// TestRun_DefaultLevelLabel — Pass 4.4 regression guard. Empty outputLevel
// must render as "[ultra]", reflecting the brand-thesis default (see
// memory/DECISIONS.md "Default output level: ultra"). A future edit that
// re-introduces "strict" as the silent fallback fails this test loudly.
func TestRun_DefaultLevelLabel(t *testing.T) {
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	out := run(t, &hookevent.Event{SessionID: "test1234", CWD: "/r"}, "")
	if !strings.Contains(out, "[ultra]") {
		t.Errorf("default level should be [ultra] (brand thesis), got: %q", out)
	}
	// Negative guard: the legacy default "[strict]" must not appear when no
	// level is supplied. Catches accidental revert to the old default.
	if strings.Contains(out, "[strict]") {
		t.Errorf("default must NOT render as [strict] (legacy default), got: %q", out)
	}
}

// --- behavioral aggregation tests ---

// Cross-session aggregation: 3 meter files for same repo, savings 100/200/300.
// Combined inSavedTokens=600. With no transcripts, denom (actual spend) is 0
// so pct rounds to 0 — verify aggregation instead via the rendered ↑600.
// Then add transcript input=900; denom = 900 (savings excluded);
// pct = round(600/900*100) = 67.
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

	out := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	if !strings.Contains(out, "↑600") {
		t.Errorf("aggregation broken; expected ↑600 in %q", out)
	}
	if extractInPct(out) != 0 {
		t.Errorf("with no spend pct should be 0, got %q", out)
	}

	// Add transcript input=900; denom = 900 (savings not in denom);
	// pct = round(600/900*100) = 67.
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 900, "output_tokens": 0},
	})
	out2 := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	pct2 := extractInPct(out2)
	if pct2 != 67 {
		t.Errorf("after adding transcript inputs, want 67 got %d (out=%q)", pct2, out2)
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

	out := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	// Only own session counted: rendered savings should be exactly 500
	// (foreign + legacy entries excluded). pct rounds to 0 because there
	// is no spend in the denominator — that is correct, not isolation
	// breakage; verify via the rendered ↑500 marker instead.
	if !strings.Contains(out, "↑500") {
		t.Errorf("isolation broken; expected ↑500 in %q", out)
	}
	if strings.Contains(out, "9999") || strings.Contains(out, "10499") {
		t.Errorf("foreign/legacy meter leaked into aggregation: %q", out)
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

	out := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
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

	out := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
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

	out := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "strict")
	if g := extractOutAbs(out); g != "3.0K" {
		t.Errorf("cumulative outAbs want 3.0K, got %q", g)
	}

	// Output absolute must NOT change with level (the post-display multiplier
	// is gone).
	outU := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "ultra")
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

	out := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/work/A"}, "strict")
	bugs := extractBugs(out)
	if bugs != 4 {
		t.Errorf("bugs all-time want 4 (1+1+2), got %d (out=%q)", bugs, out)
	}

	// Behavioral: adding more findings must increase the count.
	mk("d.jsonl", []string{hi, hi, hi})
	out2 := summary(t, &hookevent.Event{SessionID: "currentX", CWD: "/work/A"}, "strict")
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
		return summary(t, &hookevent.Event{SessionID: "varies01", CWD: "/repo/A"}, "strict")
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
		{"x ↑0 (0%) / ↓0 (25%) tokens saved y", 0, "0", "0"},
		{"x ↑5K (5%) / ↓1K (40%) tokens saved y", 5, "5K", "1K"},
		{"x ↑12.4K (43%) / ↓7.0K (40%) tokens saved y", 43, "12.4K", "7.0K"},
		{"x ↑847 (88%) / ↓100 (10%) tokens saved y", 88, "847", "100"},
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
	got := Summary(&hookevent.Event{SessionID: "sum12345", CWD: "/r"}, "strict", 0)
	if strings.Contains(got, "\033[") {
		t.Errorf("Summary should not contain ANSI escapes: %q", got)
	}
	for _, want := range []string{"tokens saved", "↑0 (0%)", "↓0 (25%)"} {
		if !strings.Contains(got, want) {
			t.Errorf("Summary missing %q: %q", want, got)
		}
	}
}

// TestStaleCompressGlyph — when stale > 0, the status-line MUST append
// "! N stale". Zero stale MUST omit the glyph. The stale count is now a
// caller-supplied int; parsing/disk-reading lives in orchestrator.CountStaleFromDisk.
func TestStaleCompressGlyph(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)

	// Case 1: stale=0 → no glyph.
	out := summary(t, &hookevent.Event{SessionID: "stale001", CWD: "/work/X"}, "strict", 0)
	if strings.Contains(out, "stale") {
		t.Errorf("stale=0 must not render stale glyph: %q", out)
	}

	// Case 2: stale=2 → "! 2 stale".
	out2 := summary(t, &hookevent.Event{SessionID: "stale002", CWD: "/work/X"}, "strict", 2)
	if !strings.Contains(out2, "! 2 stale") {
		t.Errorf("expected '! 2 stale' segment: %q", out2)
	}

	// Behavioral: stale count must vary in the rendered output.
	out3 := summary(t, &hookevent.Event{SessionID: "stale003", CWD: "/work/X"}, "strict", 5)
	if !strings.Contains(out3, "! 5 stale") {
		t.Errorf("expected '! 5 stale' segment: %q", out3)
	}
	if strings.Contains(out3, "! 2 stale") {
		t.Errorf("stale=5 must not render stale=2: %q", out3)
	}
}

// TestUnknownLevelDefaultsToUltra — invalid levels (e.g. legacy "audit")
// must render as [ultra] and not crash on a missing multiplier key.
// Pass 4.4 flipped the fallback from strict to ultra so a stale or corrupt
// config never silently downgrades compression below the brand baseline.
func TestUnknownLevelDefaultsToUltra(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	for _, bad := range []string{"audit", "fast", "garbage", "Strict"} {
		out := run(t, &hookevent.Event{SessionID: "bad12345", CWD: "/r"}, bad)
		if !strings.Contains(out, "[ultra]") {
			t.Errorf("level=%q: expected fallback to [ultra]: %q", bad, out)
		}
	}
}
