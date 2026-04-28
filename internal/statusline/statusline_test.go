package statusline

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/hookevent"
)

func TestRunOutput(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	event := &hookevent.Event{SessionID: "abc12345xyz"}
	var buf bytes.Buffer
	if err := Run(event, &buf, "strict"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Must be compact (<200 chars; ANSI escape ~20 bytes plus body).
	if len(out) >= 200 {
		t.Errorf("output too long (%d chars): %q", len(out), out)
	}

	// No trailing newline.
	if len(out) > 0 && out[len(out)-1] == '\n' {
		t.Errorf("output must not end with newline: %q", out)
	}

	// Must contain key markers.
	if !strings.Contains(out, "pakka") {
		t.Errorf("missing 'pakka' in %q", out)
	}
	if !strings.Contains(out, "tok saved") {
		t.Errorf("missing 'tok saved' in %q", out)
	}
	if !strings.Contains(out, "bugs caught") {
		t.Errorf("missing 'bugs caught' in %q", out)
	}
	if !strings.Contains(out, "[strict]") {
		t.Errorf("missing '[strict]' in %q", out)
	}
	if !strings.Contains(out, "↓") || !strings.Contains(out, "↑") {
		t.Errorf("expected UTF-8 arrows in %q", out)
	}
}

func TestRunDefaultCompressMode(t *testing.T) {
	event := &hookevent.Event{SessionID: "test1234"}
	var buf bytes.Buffer
	if err := Run(event, &buf, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "[strict]") {
		t.Error("empty compressMode should default to '[strict]'")
	}
}

func TestRunAsciiFallback(t *testing.T) {
	// Force non-UTF-8 locale.
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "C")
	t.Setenv("LC_CTYPE", "")

	event := &hookevent.Event{SessionID: "ascii123"}
	var buf bytes.Buffer
	if err := Run(event, &buf, "strict"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if strings.Contains(out, "↓") || strings.Contains(out, "↑") {
		t.Errorf("expected ascii fallback, got UTF-8 arrows: %q", out)
	}
	if !strings.Contains(out, "in ") {
		t.Errorf("ascii fallback missing 'in ': %q", out)
	}
	if !strings.Contains(out, "out ") {
		t.Errorf("ascii fallback missing 'out ': %q", out)
	}
	if !strings.Contains(out, "|") {
		t.Errorf("ascii fallback should use '|' separator: %q", out)
	}
}

func TestRunOutputUnknown(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	// No transcript path, no meter file → output unmeasured.
	event := &hookevent.Event{SessionID: "unkn0wn1"}
	var buf bytes.Buffer
	if err := Run(event, &buf, "strict"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "↑-- (--)") {
		t.Errorf("expected '↑-- (--)' for unknown output, got: %q", out)
	}
}

func TestRunHidePercentWhenZero(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	event := &hookevent.Event{SessionID: "zero1234"}
	var buf bytes.Buffer
	if err := Run(event, &buf, "strict"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// With no meter data input savings is 0 — should not print "(0%)".
	if strings.Contains(out, "↓0 (0%)") {
		t.Errorf("should not print (0%%) when nothing happened: %q", out)
	}
}

// TestRunInputPercentWhenNonZero exercises the inPct branch with real meter data.
func TestRunInputPercentWhenNonZero(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, ".pakka", "meter")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// 100 used + 100 saved → 50% input savings.
	line := `{"ts":"2025-01-01T00:00:00Z","session_id":"pct12345","tokens_used":100,"tokens_saved_est":100}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "pct12345.jsonl"), []byte(line), 0600); err != nil {
		t.Fatal(err)
	}

	event := &hookevent.Event{SessionID: "pct12345"}
	var buf bytes.Buffer
	if err := Run(event, &buf, "strict"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "(50%)") {
		t.Errorf("expected '(50%%)' input savings, got: %q", out)
	}
}

// TestRunOutputMultipliers verifies each mode's multiplier is applied
// to transcript output tokens.
func TestRunOutputMultipliers(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")

	// Build a minimal transcript file with 1000 output tokens total.
	tdir := t.TempDir()
	transcript := filepath.Join(tdir, "transcript.jsonl")
	body := `{"type":"assistant","message":{"usage":{"output_tokens":600}}}` + "\n" +
		`{"type":"assistant","message":{"usage":{"output_tokens":400}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		mode    string
		wantSav int64 // floor(1000 * mult)
	}{
		{"lite", 110},
		{"strict", 330},
		{"ultra", 670},
		{"audit", 0},
	}
	for _, c := range cases {
		event := &hookevent.Event{SessionID: "mult1234", TranscriptPath: transcript}
		var buf bytes.Buffer
		if err := Run(event, &buf, c.mode); err != nil {
			t.Fatalf("%s: %v", c.mode, err)
		}
		out := buf.String()
		// audit → outSaved=0, outPct=0; arrow followed by "0".
		if c.wantSav == 0 {
			if !strings.Contains(out, "↑0") {
				t.Errorf("mode=%s expected '↑0' in %q", c.mode, out)
			}
			continue
		}
		want := fmtTokens(c.wantSav)
		if !strings.Contains(out, "↑"+want) {
			t.Errorf("mode=%s expected '↑%s' in %q", c.mode, want, out)
		}
	}
}

func TestSummaryNoANSI(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	event := &hookevent.Event{SessionID: "sum12345"}
	got := Summary(event, "strict")
	if strings.Contains(got, "\033[") {
		t.Errorf("Summary should not contain ANSI escapes: %q", got)
	}
	if !strings.Contains(got, "tok saved") {
		t.Errorf("Summary missing 'tok saved': %q", got)
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

func TestFmtTokens(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1.0k"},
		{4200, "4.2k"},
		{12345, "12.3k"},
	}
	for _, tt := range tests {
		got := fmtTokens(tt.n)
		if got != tt.want {
			t.Errorf("fmtTokens(%d) = %q, want %q", tt.n, got, tt.want)
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
