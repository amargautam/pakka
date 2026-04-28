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

// run invokes Run and returns rendered string.
func run(t *testing.T, event *hookevent.Event, mode string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Run(event, &buf, mode); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func extractInPct(s string) int  { return extractPctAfter(s, "↑") }
func extractOutPct(s string) int { return extractPctAfter(s, "↓") }

func extractPctAfter(s, marker string) int {
	i := strings.Index(s, marker)
	if i < 0 {
		return -1
	}
	rest := s[i+len(marker):]
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

	if len(out) >= 200 {
		t.Errorf("output too long (%d): %q", len(out), out)
	}
	for _, want := range []string{"pakka", "tok saved", "bugs caught", "[strict]", "↑", "↓"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	for _, bad := range []string{"(--)", "--", "(0%)", "(50%)"} {
		if strings.Contains(out, bad) {
			t.Errorf("banned %q in %q", bad, out)
		}
	}
}

func TestArrowDirectionConventional(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	out := run(t, &hookevent.Event{SessionID: "arrow001", CWD: "/r"}, "strict")

	upIdx := strings.Index(out, "↑")
	downIdx := strings.Index(out, "↓")
	slashIdx := strings.Index(out, "/")
	if !(upIdx > 0 && upIdx < slashIdx && slashIdx < downIdx) {
		t.Errorf("expected ↑ < / < ↓: %q (idx %d/%d/%d)", out, upIdx, slashIdx, downIdx)
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
	for _, want := range []string{"in ", "out ", "|", "in 0%"} {
		if !strings.Contains(out, want) {
			t.Errorf("ascii missing %q in %q", want, out)
		}
	}
}

func TestRunDefaultCompressMode(t *testing.T) {
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	out := run(t, &hookevent.Event{SessionID: "test1234", CWD: "/r"}, "")
	if !strings.Contains(out, "[strict]") {
		t.Errorf("default mode should be [strict]: %q", out)
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
// outSaved = 990, denom = 3990, pct = round(990/3990*100) = 25.
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
	if extractOutPct(out) != 25 {
		t.Errorf("cumulative outPct want 25, got %q", out)
	}

	// Mode swap → outPct grows (ultra > strict).
	outU := run(t, &hookevent.Event{SessionID: "currentX", CWD: "/repo/A"}, "ultra")
	if !(extractOutPct(outU) > 25) {
		t.Errorf("ultra outPct should exceed strict (25), got %q", outU)
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
	for _, want := range []string{"tok saved", "↑0%", "↓0%"} {
		if !strings.Contains(got, want) {
			t.Errorf("Summary missing %q: %q", want, got)
		}
	}
}
