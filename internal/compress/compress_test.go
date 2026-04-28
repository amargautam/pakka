package compress

import (
	"strings"
	"testing"
)

func TestAuditPassthrough(t *testing.T) {
	input := "hello\n\n\nworld\n"
	r := Run(input, ModeAudit)
	if r.Output != input {
		t.Errorf("audit should pass through, got %q", r.Output)
	}
	if r.Ratio != 0 {
		t.Errorf("ratio = %f, want 0", r.Ratio)
	}
	if r.OriginalSize != len(input) {
		t.Errorf("original_size = %d, want %d", r.OriginalSize, len(input))
	}
}

func TestEmptyInput(t *testing.T) {
	r := Run("", ModeStrict)
	if r.Output != "" {
		t.Errorf("empty input should produce empty output, got %q", r.Output)
	}
	if r.Ratio != 0 {
		t.Errorf("ratio = %f, want 0", r.Ratio)
	}
}

func TestStrictCollapsesBlanks(t *testing.T) {
	input := "a\n\n\n\nb\n"
	r := Run(input, ModeStrict)
	if strings.Contains(r.Output, "\n\n\n") {
		t.Errorf("strict mode should collapse multiple blanks, got %q", r.Output)
	}
	if !strings.Contains(r.Output, "a\nb\n") {
		t.Errorf("strict mode should remove all blank lines, got %q", r.Output)
	}
}

func TestStrictStripsFenceHeader(t *testing.T) {
	input := "```go\nfmt.Println()\n```\n"
	r := Run(input, ModeStrict)
	if strings.Contains(r.Output, "```go") {
		t.Errorf("strict mode should strip fence language, got %q", r.Output)
	}
	if !strings.Contains(r.Output, "```\nfmt.Println()") {
		t.Errorf("code body should be preserved, got %q", r.Output)
	}
}

func TestStrictDedupsHeadings(t *testing.T) {
	input := "# Intro\nfoo\n# Intro\nbar\n"
	r := Run(input, ModeStrict)
	if strings.Count(r.Output, "# Intro") != 1 {
		t.Errorf("expected 1 heading, got %q", r.Output)
	}
	if !strings.Contains(r.Output, "foo") || !strings.Contains(r.Output, "bar") {
		t.Errorf("non-heading content should be preserved, got %q", r.Output)
	}
}

func TestStrictRemovesAllBlanks(t *testing.T) {
	input := "a\n\n\nb\n\nc\n"
	r := Run(input, ModeStrict)
	if strings.Contains(r.Output, "\n\n") {
		t.Errorf("strict should remove all blank lines, got %q", r.Output)
	}
}

func TestStrictCollapsesWhitespace(t *testing.T) {
	input := "hello    world\n"
	r := Run(input, ModeStrict)
	if strings.Contains(r.Output, "    ") {
		t.Errorf("strict should collapse inline whitespace, got %q", r.Output)
	}
	if !strings.Contains(r.Output, "hello world") {
		t.Errorf("expected collapsed whitespace, got %q", r.Output)
	}
}

func TestCriticalMarkersPreserved(t *testing.T) {
	markers := []string{"TODO", "FIXME", "SECURITY", "HACK", "BUG", "XXX"}
	for _, m := range markers {
		input := "a\n" + m + ": fix this\nb\n"
		r := Run(input, ModeStrict)
		if !strings.Contains(r.Output, m+": fix this") {
			t.Errorf("marker %s should be preserved, got %q", m, r.Output)
		}
	}
}

func TestCodeBlockVerbatim(t *testing.T) {
	input := "```\n# not a heading\n\n\nempty lines inside\n```\n"
	r := Run(input, ModeStrict)
	if !strings.Contains(r.Output, "# not a heading") {
		t.Errorf("code block content should be verbatim, got %q", r.Output)
	}
	if !strings.Contains(r.Output, "\n\n\nempty lines inside") {
		t.Errorf("blank lines inside code block should be preserved, got %q", r.Output)
	}
}

func TestRatioCalculation(t *testing.T) {
	// Build input with lots of blank lines that will be compressed
	input := strings.Repeat("content\n\n\n\n\n", 20)
	r := Run(input, ModeStrict)
	if r.Ratio <= 0 {
		t.Errorf("ratio should be positive, got %f", r.Ratio)
	}
	if r.CompressedSize >= r.OriginalSize {
		t.Errorf("compressed (%d) should be smaller than original (%d)", r.CompressedSize, r.OriginalSize)
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		in   string
		want Mode
	}{
		{"strict", ModeStrict},
		{"audit", ModeAudit},
		{"", ModeStrict},
		{"unknown", ModeStrict},
	}
	for _, tt := range tests {
		got := ParseMode(tt.in)
		if got != tt.want {
			t.Errorf("ParseMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatRatio(t *testing.T) {
	r := &Result{OriginalSize: 8300, CompressedSize: 4800, Ratio: 42.1}
	s := FormatRatio(r)
	if !strings.Contains(s, "42.1%") {
		t.Errorf("should contain ratio, got %q", s)
	}
	if !strings.Contains(s, "8.3k") {
		t.Errorf("should contain original size, got %q", s)
	}
	if !strings.Contains(s, "4.8k") {
		t.Errorf("should contain compressed size, got %q", s)
	}
}

func TestHeadingDedup_CaseSensitive(t *testing.T) {
	input := "# Intro\nfoo\n# intro\nbar\n"
	r := Run(input, ModeStrict)
	// Both should be deduplicated (case-insensitive)
	if strings.Count(r.Output, "ntro") != 1 {
		t.Errorf("heading dedup should be case-insensitive, got %q", r.Output)
	}
}

func TestTildeCodeFence(t *testing.T) {
	input := "~~~python\nprint('hi')\n~~~\n"
	r := Run(input, ModeStrict)
	if strings.Contains(r.Output, "python") {
		t.Errorf("should strip tilde fence language, got %q", r.Output)
	}
	if !strings.Contains(r.Output, "print('hi')") {
		t.Errorf("code body should be preserved, got %q", r.Output)
	}
}
