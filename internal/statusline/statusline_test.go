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

// runWith builds an event with the given session id, writes a meter file with
// the given (used, savedEst) pair into HOME/.pakka/meter, and returns rendered
// status line string. Caller must have already set HOME via t.Setenv.
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

// extractInPct parses ↓N% out of the rendered line. Returns -1 on miss.
func extractInPct(s string) int {
	return extractPctAfter(s, "↓")
}

// extractOutPct parses ↑N% out of the rendered line. Returns -1 on miss.
func extractOutPct(s string) int {
	return extractPctAfter(s, "↑")
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

	// Compact (<200 chars; ANSI ~20 bytes plus body).
	if len(out) >= 200 {
		t.Errorf("output too long (%d chars): %q", len(out), out)
	}
	// No trailing newline.
	if len(out) > 0 && out[len(out)-1] == '\n' {
		t.Errorf("output must not end with newline: %q", out)
	}
	// Must contain key markers.
	for _, want := range []string{"pakka", "tok saved", "bugs caught", "[strict]", "↓", "↑"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}

	// New format: no `(--)` placeholder, no raw count abbreviations.
	if strings.Contains(out, "(--)") || strings.Contains(out, "--") {
		t.Errorf("legacy '--' placeholder must not appear: %q", out)
	}
	if strings.Contains(out, "k tok") || strings.Contains(out, "k /") {
		t.Errorf("raw kilo-token counts must not appear: %q", out)
	}
	// Parens around (NN%) — also gone.
	if strings.Contains(out, "(0%)") || strings.Contains(out, "(50%)") {
		t.Errorf("legacy parenthesised pct must not appear: %q", out)
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
	// New ascii shape includes "in 0%" / "out 0%".
	if !strings.Contains(out, "in 0%") {
		t.Errorf("ascii fallback should render 'in 0%%' when unmeasured: %q", out)
	}
}

// Output unmeasured (no transcript, no meter) must render '↑0%' — never '--'.
func TestRunOutputUnknownRendersZeroPct(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())
	out := runWith(t, "unkn0wn1", "strict", "")
	if !strings.Contains(out, "↑0%") {
		t.Errorf("expected '↑0%%' for unknown output, got: %q", out)
	}
	if strings.Contains(out, "--") {
		t.Errorf("must NOT contain '--' placeholder: %q", out)
	}
}

// Behavioral: increasing tokens_saved_est while holding tokens_used constant
// must INCREASE inPct.
func TestInPctIncreasesWithSavings(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMeter(t, home, "lowSv001", 1000, 100) // savedEst = 100
	writeMeter(t, home, "highSv01", 1000, 900) // savedEst = 900
	low := extractInPct(runWith(t, "lowSv001", "strict", ""))
	high := extractInPct(runWith(t, "highSv01", "strict", ""))

	if low < 0 || high < 0 {
		t.Fatalf("could not parse inPct: low=%d high=%d", low, high)
	}
	if !(high > low) {
		t.Errorf("inPct should increase with savings: low=%d high=%d", low, high)
	}
	// Sanity: 100/(1000+100) ≈ 9% rounded; 900/1900 ≈ 47% rounded.
	if low != 9 {
		t.Errorf("low inPct: want 9, got %d", low)
	}
	if high != 47 {
		t.Errorf("high inPct: want 47, got %d", high)
	}
}

// Behavioral: doubling tokens_used while holding savings constant must
// DECREASE inPct.
func TestInPctDecreasesWithUsage(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMeter(t, home, "useLo001", 500, 500)  // 50/50 → 50%
	writeMeter(t, home, "useHi001", 1500, 500) // 25/75 → 25%
	lo := extractInPct(runWith(t, "useLo001", "strict", ""))
	hi := extractInPct(runWith(t, "useHi001", "strict", ""))

	if lo < 0 || hi < 0 {
		t.Fatalf("could not parse inPct: lo=%d hi=%d", lo, hi)
	}
	if !(lo > hi) {
		t.Errorf("inPct should decrease as usage grows: lo=%d hi=%d", lo, hi)
	}
}

// Behavioral: 0.4% rounds to 0%, 0.6% rounds to 1%, 24.6% rounds to 25%.
// Together these prove rendering uses ROUND, not TRUNC.
func TestInPctIsRounded(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	// 4 saved / (4+996) = 0.4% → rounds to 0
	writeMeter(t, home, "rndDn001", 996, 4)
	// 6 saved / (6+994) = 0.6% → rounds to 1 (would be 0 with TRUNC)
	writeMeter(t, home, "rndUp001", 994, 6)
	// 246 / (246+754) = 24.6% → rounds to 25 (TRUNC would give 24)
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

// Behavioral: outPct grows monotonically with mode coefficient. Same transcript;
// only the mode changes. lite < strict < ultra; audit = 0.
func TestOutPctMonotonicByMode(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())

	tdir := t.TempDir()
	transcript := filepath.Join(tdir, "transcript.jsonl")
	body := `{"type":"assistant","message":{"usage":{"output_tokens":600}}}` + "\n" +
		`{"type":"assistant","message":{"usage":{"output_tokens":400}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

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
	// Sanity: strict ≈ 0.33/(1+0.33) ≈ 25%; ultra ≈ 0.67/1.67 ≈ 40%; lite ≈ 0.11/1.11 ≈ 10%.
	if pcts["strict"] < 20 || pcts["strict"] > 30 {
		t.Errorf("strict outPct out of expected band: %d", pcts["strict"])
	}
}

// Behavioral: unmeasured output and measured-but-rounds-down-to-0% render
// identically. (Both '↑0%'.) Intentional per Pass 4.x decision.
func TestUnmeasuredAndZeroOutRenderIdentically(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("HOME", t.TempDir())

	// Unmeasured: no transcript path.
	unmeasured := runWith(t, "unmeas01", "strict", "")

	// Measured but rounds to 0%: audit mode (multiplier=0) over a transcript.
	tdir := t.TempDir()
	transcript := filepath.Join(tdir, "transcript.jsonl")
	body := `{"type":"assistant","message":{"usage":{"output_tokens":1000}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	measured := runWith(t, "meas0001", "audit", transcript)

	if !strings.Contains(unmeasured, "↑0%") {
		t.Errorf("unmeasured must render ↑0%%: %q", unmeasured)
	}
	if !strings.Contains(measured, "↑0%") {
		t.Errorf("measured-zero must render ↑0%%: %q", measured)
	}
	// Neither path may emit '--'.
	for _, s := range []string{unmeasured, measured} {
		if strings.Contains(s, "--") {
			t.Errorf("'--' must not appear: %q", s)
		}
	}
}

// Behavioral: no rendered output ever contains '(--)', '--', or kilo-token
// abbreviations like '1.7k'. Sweep across modes and conditions.
func TestNoLegacyTokensInOutput(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMeter(t, home, "sweep001", 5000, 1700)

	tdir := t.TempDir()
	transcript := filepath.Join(tdir, "transcript.jsonl")
	body := `{"type":"assistant","message":{"usage":{"output_tokens":4200}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{"lite", "strict", "ultra", "audit"} {
		out := runWith(t, "sweep001", mode, transcript)
		// Banned substrings.
		for _, bad := range []string{"(--)", "1.7k", "4.2k", "5.0k", "(0%)", "(50%)"} {
			if strings.Contains(out, bad) {
				t.Errorf("mode=%s: banned %q appears in %q", mode, bad, out)
			}
		}
		// Must contain percent + slash + tok saved.
		if !strings.Contains(out, "% / ") {
			t.Errorf("mode=%s: missing '%% / ' in %q", mode, out)
		}
	}
}

// TestRunInputPercentWhenNonZero verifies the inPct branch with real meter
// data. 100/(100+100) = 50% — and the format must be '↓50%' (no parens, no count).
func TestRunInputPercentWhenNonZero(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeMeter(t, home, "pct12345", 100, 100)

	out := runWith(t, "pct12345", "strict", "")
	if !strings.Contains(out, "↓50%") {
		t.Errorf("expected '↓50%%' in %q", out)
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
	// Summary must follow the same percent-only format.
	if !strings.Contains(got, "↓0%") || !strings.Contains(got, "↑0%") {
		t.Errorf("Summary must use percent-only format: %q", got)
	}
}

func TestReadTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	body := `{"type":"assistant","message":{"usage":{"output_tokens":500}}}` + "\n" +
		`{"type":"user","message":{}}` + "\n" +
		`{"usage":{"output_tokens":120}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got, ok := readTranscript(path)
	if !ok {
		t.Fatal("readTranscript returned ok=false")
	}
	if got != 620 {
		t.Errorf("readTranscript = %d, want 620", got)
	}
}

func TestReadTranscriptMissing(t *testing.T) {
	if got, ok := readTranscript("/nonexistent/path.jsonl"); ok || got != 0 {
		t.Errorf("readTranscript on missing = (%d, %v), want (0, false)", got, ok)
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

// pctRound: behavioral — output must (a) be 0 when denom is 0, (b) increase
// monotonically with num for fixed denom, (c) match math.Round semantics.
func TestPctRound(t *testing.T) {
	tests := []struct {
		num, denom int64
		want       int64
	}{
		{0, 0, 0},      // guard
		{5, 0, 0},      // guard
		{0, 100, 0},    // exact
		{4, 1000, 0},   // 0.4 → 0
		{6, 1000, 1},   // 0.6 → 1
		{246, 1000, 25},
		{500, 1000, 50},
		{999, 1000, 100}, // 99.9 → 100
		{1000, 1000, 100},
	}
	for _, tt := range tests {
		got := pctRound(tt.num, tt.denom)
		if got != tt.want {
			t.Errorf("pctRound(%d,%d) = %d, want %d", tt.num, tt.denom, got, tt.want)
		}
	}

	// Monotonicity: prev < curr for increasing num at fixed denom.
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
