package pricing

import (
	"testing"
)

func TestLookupKnownModel(t *testing.T) {
	cases := []struct {
		model string
		want  ModelPrices
	}{
		{"claude-opus-4-7", ModelPrices{5.00, 25.00, 6.25, 0.50}},
		{"claude-opus-4-6", ModelPrices{5.00, 25.00, 6.25, 0.50}},
		{"claude-opus-4-5", ModelPrices{5.00, 25.00, 6.25, 0.50}},
		{"claude-sonnet-4-6", ModelPrices{3.00, 15.00, 3.75, 0.30}},
		{"claude-sonnet-4-5", ModelPrices{3.00, 15.00, 3.75, 0.30}},
		{"claude-haiku-4-5-20251001", ModelPrices{1.00, 5.00, 1.25, 0.10}},
		{"claude-haiku-3-5-20241022", ModelPrices{0.80, 4.00, 1.00, 0.08}},
	}
	for _, c := range cases {
		got := Lookup(c.model)
		if got != c.want {
			t.Errorf("Lookup(%q) = %+v, want %+v", c.model, got, c.want)
		}
	}
}

func TestLookupUnknownModelReturnsDefault(t *testing.T) {
	for _, unknown := range []string{"", "claude-unknown-99", "gpt-4", "random-model"} {
		got := Lookup(unknown)
		if got != Default {
			t.Errorf("Lookup(%q) = %+v, want Default %+v", unknown, got, Default)
		}
	}
}

func TestSessionCostUSDZeroTokens(t *testing.T) {
	got := SessionCostUSD(Default, 0, 0, 0, 0)
	if got != 0.0 {
		t.Errorf("SessionCostUSD zero tokens = %v, want 0.0", got)
	}
}

func TestSessionCostUSDKnownValues(t *testing.T) {
	// Sonnet 4.6: Input=3.00, Output=15.00, CacheWrite=3.75, CacheRead=0.30 per 1M tokens.
	// 1M input tokens → $3.00
	// 1M output tokens → $15.00
	// 1M cache write tokens → $3.75
	// 1M cache read tokens → $0.30
	p := Lookup("claude-sonnet-4-6")

	cases := []struct {
		name                              string
		inputTok, cacheWrite, cacheRead   int64
		outputTok                         int64
		want                              float64
	}{
		{
			name:      "1M input only",
			inputTok:  1_000_000,
			want:      3.00,
		},
		{
			name:      "1M output only",
			outputTok: 1_000_000,
			want:      15.00,
		},
		{
			name:       "1M cache write only",
			cacheWrite: 1_000_000,
			want:       3.75,
		},
		{
			name:      "1M cache read only",
			cacheRead: 1_000_000,
			want:      0.30,
		},
		{
			name:       "mixed: 100K input + 50K output + 200K cache write + 500K cache read",
			inputTok:   100_000,
			outputTok:  50_000,
			cacheWrite: 200_000,
			cacheRead:  500_000,
			// = (100000*3 + 200000*3.75 + 500000*0.30 + 50000*15) / 1M
			// = (300000 + 750000 + 150000 + 750000) / 1M
			// = 1950000 / 1M = 1.95
			want: 1.95,
		},
	}
	for _, c := range cases {
		got := SessionCostUSD(p, c.inputTok, c.cacheWrite, c.cacheRead, c.outputTok)
		// Use a small epsilon for float comparison.
		const eps = 1e-9
		diff := got - c.want
		if diff < -eps || diff > eps {
			t.Errorf("SessionCostUSD %s = %.6f, want %.6f", c.name, got, c.want)
		}
	}
}

func TestSessionCostUSDOpusPrices(t *testing.T) {
	p := Lookup("claude-opus-4-7")
	// 1M input ($5.00) + 1M output ($25.00) = $30.00
	got := SessionCostUSD(p, 1_000_000, 0, 0, 1_000_000)
	const want = 30.00
	const eps = 1e-9
	if diff := got - want; diff < -eps || diff > eps {
		t.Errorf("Opus 1M+1M = %.6f, want %.6f", got, want)
	}
}

func TestFormatUSD(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.001, "$0.00"},
		{0.034, "$0.03"},
		{1.5, "$1.50"},
		{0.0, "$0.00"},
		{12.5, "$12.50"},
		{1.24, "$1.24"},
		{-1.0, "$0.00"}, // negative clamped
	}
	for _, c := range cases {
		got := FormatUSD(c.in)
		if got != c.want {
			t.Errorf("FormatUSD(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
