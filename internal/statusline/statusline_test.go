package statusline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/hookevent"
)

// runWith builds an event with the given session id, optional transcript path,
// and renders status line via Run. Caller must have set HOME via t.Setenv.
func runWith(t *testing.T, sid, mode string, transcriptPath string) string {
	t.Helper()
	event := &hookevent.Event{SessionID: sid, TranscriptPath: transcriptPath}
	var buf bytes.Buffer
	if err := Run(event, &buf, mode); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// writeMeter writes a single meter JSONL entry sufficient for compute() to
// pick up the (tokens_used, tokens_saved_est) values.
func writeMeter(t *testing.T, home, sid string, used, savedEst int64) {
	t.Helper()
	dir := filepath.Join(home, ".pakka", "meter")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(
		`{"ts":"2025-01-01T00:00:00Z","session_id":%q,"tokens_used":%d,"tokens_saved_est":%d}`+"\n",
		sid, used, savedEst,
	)
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
}

// writeTranscript writes a JSONL transcript file with assistant turns whose
// usage fields produce the requested (input_tokens, output_tokens) totals.
// Splits across two turns to exercise summing.
func writeTranscript(t *testing.T, dir string, inTokens, outTokens int64) string {
	t.Helper()
	p := filepath.Join(dir, "transcript.jsonl")
	in1 := inTokens / 2
	in2 := inTokens - in1
	out1 := outTokens / 2
	out2 := outTokens - out1
	body := fmt.Sprintf(
		`{"type":"assistant","message":{"usage":{"input_tokens":%d,"output_tokens":%d}}}`+"\n"+
			`{"type":"assistant","message":{"usage":{"input_tokens":%d,"output_tokens":%d}}}`+"\n",
		in1, out1, in2, out2,
	)
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

// extractInPct parses ↑N% (the input arrow under new convention).
func extractInPct(s string) int {
	return extractPctAfter(s, "↑")
}

// extractOutPct parses ↓N% (the output arrow under new convention).
func extractOutPct(s string) int {
	return extractPctAfter(s, "↓")
}

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

func TestRunOutput(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())
	out := runWith(t, "abc12345xyz", "strict", "")

	if len(out) >= 200 {
		t.Errorf("output too long (%d chars): %q", len(out), out)
	}
	if len(out) > 0 && out[len(out)-1] == '\n' {
		t.Errorf("output must not end with newline: %q", out)
	}
	for _, want := range []string{"pakka", "tok saved", "bugs caught", "[strict]", "↑", "↓"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "(--)") || strings.Contains(out, "--") {
		t.Errorf("legacy '--' placeholder must not appear: %q", out)
	}
	if strings.Contains(out, "(0%)") || strings.Contains(out, "(50%)") {
		t.Errorf("legacy parenthesised pct must not appear: %q", out)
	}
}

// Bug 1 regression: arrows follow conventional download/upload semantics.
// ↑ precedes the input pct; ↓ precedes the output pct. The arrow ordering
// in the rendered string is ↑...% / ↓...%.
func TestArrowDirectionConventional(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())
	out := runWith(t, "arrow001", "strict", "")

	upIdx := strings.Index(out, "↑")
	downIdx := strings.Index(out, "↓")
	slashIdx := strings.Index(out, "/")
	if upIdx < 0 || downIdx < 0 || slashIdx < 0 {
		t.Fatalf("missing arrows or separator: %q", out)
	}
	if !(upIdx < slashIdx && slashIdx < downIdx) {
		t.Errorf("expected order ↑ < / < ↓, got upIdx=%d slashIdx=%d downIdx=%d in %q",
			upIdx, slashIdx, downIdx, out)
	}
	if extractInPct(out) < 0 {
		t.Errorf("↑ not followed by N%%: %q", out)
	}
	if extractOutPct(out) < 0 {
		t.Errorf("↓ not followed by N%%: %q", out)
	}
}

func TestRunDefaultCompressMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out := runWith(t, "test1234", "", "")
	if !strings.Contains(out, "[strict]") {
		t.Error("empty compressMode should default to '[strict]'")
	}
}

func TestRunAsciiFallback(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "C")
	t.Setenv("LC_CTYPE", "")
	t.Setenv("HOME", t.TempDir())

	out := runWith(t, "ascii123", "strict", "")
	if strings.Contains(out, "↓") || strings.Contains(out, "↑") {
		t.Errorf("expected ascii fallback, got UTF-8 arrows: %q", out)
	}
	for _, want := range []string{"in ", "out ", "|"} {
		if !strings.Contains(out, want) {
			t.Errorf("ascii fallback missing %q in %q", want, out)
		}
	}
	if !strings.Contains(out, "in 0%") {
		t.Errorf("ascii fallback should render 'in 0%%' when unmeasured: %q", out)
	}
	inIdx := strings.Index(out, "in ")
	outIdx := strings.Index(out, "out ")
	if !(inIdx > 0 && outIdx > inIdx) {
		t.Errorf("ascii: 'in ' should precede 'out ': %q", out)
	}
}

// Output unmeasured (no transcript, no meter) must render '↓0%'.
func TestRunOutputUnknownRendersZeroPct(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())
	out := runWith(t, "unkn0wn1", "strict", "")
	if !strings.Contains(out, "↓0%") {
		t.Errorf("expected '↓0%%' for unknown output, got: %q", out)
	}
	if strings.Contains(out, "--") {
		t.Errorf("must NOT contain '--' placeholder: %q", out)
	}
}

// Bug 2 regression: input % must update from transcript across turns even
// when no tool calls fire (meter tokensUsed stays 0). Hold tokensSavedEst
// constant; growing transcript input → smaller inPct.
func TestInputPctUpdatesFromTranscriptNoToolCalls(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	const sid = "trnsIn01"
	// Meter has only tokens_saved_est (e.g., from a CLAUDE.md compress); no
	// tool calls in this scenario, so tokens_used stays 0.
	writeMeter(t, home, sid, 0, 200)

	dir1 := t.TempDir()
	dir2 := t.TempDir()
	t1 := writeTranscript(t, dir1, 800, 100)
	t2 := writeTranscript(t, dir2, 5000, 100)

	pct1 := extractInPct(runWith(t, sid, "strict", t1))
	pct2 := extractInPct(runWith(t, sid, "strict", t2))

	if pct1 < 0 || pct2 < 0 {
		t.Fatalf("could not parse inPct: pct1=%d pct2=%d", pct1, pct2)
	}
	if !(pct1 > pct2) {
		t.Errorf("inPct should decrease as transcript input grows: pct1=%d pct2=%d", pct1, pct2)
	}
	if pct1 == 0 {
		t.Errorf("expected non-zero inPct on small transcript: %d", pct1)
	}
}

// Bug 2 follow-up: inPct must rise monotonically with savings while transcript
// input is held constant.
func TestInputPctRisesWithSavings(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	tdir := t.TempDir()
	transcript := writeTranscript(t, tdir, 1000, 0)

	writeMeter(t, home, "savLow01", 0, 100)
	writeMeter(t, home, "savHi001", 0, 900)

	low := extractInPct(runWith(t, "savLow01", "strict", transcript))
	high := extractInPct(runWith(t, "savHi001", "strict", transcript))

	if low < 0 || high < 0 {
		t.Fatalf("parse fail: low=%d high=%d", low, high)
	}
	if !(high > low) {
		t.Errorf("inPct should rise with savings: low=%d high=%d", low, high)
	}
	if low != 9 {
		t.Errorf("low inPct: want 9, got %d", low)
	}
	if high != 47 {
		t.Errorf("high inPct: want 47, got %d", high)
	}
}

// Bug 2 hierarchy: when transcript has inTokens, meter tokensUsed is ignored.
func TestTranscriptWinsOverMeterForInput(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	const sid = "winsT001"
	writeMeter(t, home, sid, 1_000_000, 1000)
	tdir := t.TempDir()
	transcript := writeTranscript(t, tdir, 1000, 0)

	pct := extractInPct(runWith(t, sid, "strict", transcript))
	if pct < 0 {
		t.Fatalf("parse fail: %d", pct)
	}
	if pct != 50 {
		t.Errorf("expected transcript-driven inPct=50, got %d (meter contamination?)", pct)
	}
	if pct < 5 {
		t.Errorf("inPct=%d looks meter-driven; transcript should win", pct)
	}
}

// Bug 2 fallback: when transcript missing/unparseable, meter tokensUsed IS used.
func TestMeterFallbackForInputWhenNoTranscript(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMeter(t, home, "fbk00001", 1000, 1000) // 50/50 → 50%
	pctNoTranscript := extractInPct(runWith(t, "fbk00001", "strict", ""))
	if pctNoTranscript != 50 {
		t.Errorf("meter fallback (no transcript): want 50, got %d", pctNoTranscript)
	}

	writeMeter(t, home, "fbk00002", 2000, 1000) // 1000/3000 → 33%
	pctMissing := extractInPct(runWith(t, "fbk00002", "strict", "/nonexistent/x.jsonl"))
	if pctMissing != 33 {
		t.Errorf("missing-transcript fallback: want 33, got %d", pctMissing)
	}
}

// Behavioral: 0.4% rounds to 0%, 0.6% rounds to 1%, 24.6% rounds to 25%.
func TestInPctIsRounded(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMeter(t, home, "rndDn001", 996, 4)
	writeMeter(t, home, "rndUp001", 994, 6)
	writeMeter(t, home, "rndUp246", 754, 246)

	dn := extractInPct(runWith(t, "rndDn001", "strict", ""))
	up := extractInPct(runWith(t, "rndUp001", "strict", ""))
	mid := extractInPct(runWith(t, "rndUp246", "strict", ""))

	if dn != 0 {
		t.Errorf("0.4%% should round to 0, got %d", dn)
	}
	if up != 1 {
		t.Errorf("0.6%% should round to 1, got %d (TRUNC bug?)", up)
	}
	if mid != 25 {
		t.Errorf("24.6%% should round to 25, got %d (TRUNC bug?)", mid)
	}
}

// outPct grows monotonically with mode coefficient.
func TestOutPctMonotonicByMode(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())

	tdir := t.TempDir()
	transcript := writeTranscript(t, tdir, 0, 1000)

	pcts := map[string]int{}
	for _, mode := range []string{"audit", "lite", "strict", "ultra"} {
		out := runWith(t, "mult1234", mode, transcript)
		pcts[mode] = extractOutPct(out)
		if pcts[mode] < 0 {
			t.Fatalf("could not parse outPct for mode=%s: %q", mode, out)
		}
	}

	if pcts["audit"] != 0 {
		t.Errorf("audit mode outPct want 0, got %d", pcts["audit"])
	}
	if !(pcts["lite"] < pcts["strict"] && pcts["strict"] < pcts["ultra"]) {
		t.Errorf("expected lite<strict<ultra, got %v", pcts)
	}
	if pcts["strict"] < 20 || pcts["strict"] > 30 {
		t.Errorf("strict outPct out of expected band: %d", pcts["strict"])
	}
}

// Unmeasured output and measured-but-rounds-to-0% render identically.
func TestUnmeasuredAndZeroOutRenderIdentically(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())

	unmeasured := runWith(t, "unmeas01", "strict", "")

	tdir := t.TempDir()
	transcript := writeTranscript(t, tdir, 0, 1000)
	measured := runWith(t, "meas0001", "audit", transcript)

	if !strings.Contains(unmeasured, "↓0%") {
		t.Errorf("unmeasured must render ↓0%%: %q", unmeasured)
	}
	if !strings.Contains(measured, "↓0%") {
		t.Errorf("measured-zero must render ↓0%%: %q", measured)
	}
	for _, s := range []string{unmeasured, measured} {
		if strings.Contains(s, "--") {
			t.Errorf("'--' must not appear: %q", s)
		}
	}
}

// Sweep: no rendered output ever contains '(--)', '--', or count-with-pct.
func TestNoLegacyTokensInOutput(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMeter(t, home, "sweep001", 5000, 1700)
	tdir := t.TempDir()
	transcript := writeTranscript(t, tdir, 0, 4200)

	for _, mode := range []string{"lite", "strict", "ultra", "audit"} {
		out := runWith(t, "sweep001", mode, transcript)
		for _, bad := range []string{"(--)", "1.7k", "4.2k", "5.0k", "(0%)", "(50%)"} {
			if strings.Contains(out, bad) {
				t.Errorf("mode=%s: banned %q appears in %q", mode, bad, out)
			}
		}
		if !strings.Contains(out, "% / ") {
			t.Errorf("mode=%s: missing '%% / ' in %q", mode, out)
		}
	}
}

// 100/(100+100) = 50% — format must be '↑50%' (no parens, no count).
func TestRunInputPercentWhenNonZero(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMeter(t, home, "pct12345", 100, 100)

	out := runWith(t, "pct12345", "strict", "")
	if !strings.Contains(out, "↑50%") {
		t.Errorf("expected '↑50%%' in %q", out)
	}
	if strings.Contains(out, "(50%)") {
		t.Errorf("legacy '(50%%)' format must not appear: %q", out)
	}
}

func TestSummaryNoANSI(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())
	got := Summary(&hookevent.Event{SessionID: "sum12345"}, "strict")
	if strings.Contains(got, "\033[") {
		t.Errorf("Summary should not contain ANSI escapes: %q", got)
	}
	if !strings.Contains(got, "tok saved") {
		t.Errorf("Summary missing 'tok saved': %q", got)
	}
	if !strings.Contains(got, "↑0%") || !strings.Contains(got, "↓0%") {
		t.Errorf("Summary must use percent-only format with conventional arrows: %q", got)
	}
}

// readTranscript: behavioral — input sums input_tokens + cache reads + cache
// creation; output sums output_tokens. Two candidate JSON shapes supported.
func TestReadTranscriptSumsInAndOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	body := `{"type":"assistant","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":50,"cache_creation_input_tokens":25,"output_tokens":500}}}` + "\n" +
		`{"type":"user","message":{}}` + "\n" +
		`{"usage":{"input_tokens":10,"output_tokens":120}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	in, out, ok := readTranscript(path)
	if !ok {
		t.Fatal("readTranscript returned ok=false")
	}
	if in != 185 {
		t.Errorf("readTranscript inTokens = %d, want 185", in)
	}
	if out != 620 {
		t.Errorf("readTranscript outTokens = %d, want 620", out)
	}
}

// Behavioral: doubling input_tokens in transcript → readTranscript inTokens
// also doubles. Proves the value VARIES with input.
func TestReadTranscriptVariesWithInput(t *testing.T) {
	for _, n := range []int64{100, 200, 1000} {
		dir := t.TempDir()
		path := filepath.Join(dir, "t.jsonl")
		body := fmt.Sprintf(`{"type":"assistant","message":{"usage":{"input_tokens":%d,"output_tokens":1}}}`+"\n", n)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		in, _, ok := readTranscript(path)
		if !ok {
			t.Fatalf("ok=false for n=%d", n)
		}
		if in != n {
			t.Errorf("readTranscript inTokens = %d, want %d", in, n)
		}
	}
}

func TestReadTranscriptMissing(t *testing.T) {
	in, out, ok := readTranscript("/nonexistent/path.jsonl")
	if ok || in != 0 || out != 0 {
		t.Errorf("readTranscript on missing = (%d, %d, %v), want (0, 0, false)", in, out, ok)
	}
}

func TestShortSID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"abc12345xyz", "abc12345"},
		{"short", "short"},
		{"exactly8", "exactly8"},
		{"", ""},
	}
	for _, tt := range tests {
		got := shortSID(tt.in)
		if got != tt.want {
			t.Errorf("shortSID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPctRound(t *testing.T) {
	tests := []struct {
		num, denom int64
		want       int64
	}{
		{0, 0, 0},
		{5, 0, 0},
		{0, 100, 0},
		{4, 1000, 0},
		{6, 1000, 1},
		{246, 1000, 25},
		{500, 1000, 50},
		{999, 1000, 100},
		{1000, 1000, 100},
	}
	for _, tt := range tests {
		got := pctRound(tt.num, tt.denom)
		if got != tt.want {
			t.Errorf("pctRound(%d,%d) = %d, want %d", tt.num, tt.denom, got, tt.want)
		}
	}

	const denom int64 = 1000
	var prev int64 = -1
	for n := int64(0); n <= 1000; n += 100 {
		got := pctRound(n, denom)
		if got < prev {
			t.Errorf("pctRound non-monotonic: n=%d got=%d prev=%d", n, got, prev)
		}
		prev = got
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
