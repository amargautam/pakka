package pricing

import (
	"testing"
)

func TestLookupKnownModel(t *testing.T) {
	cases := []struct {
		model string
		want  ModelPrices
	}{
		{"claude-fable-5", ModelPrices{10.00, 50.00, 12.50, 1.00}},
		{"claude-mythos-5", ModelPrices{10.00, 50.00, 12.50, 1.00}},
		{"claude-opus-4-8", ModelPrices{5.00, 25.00, 6.25, 0.50}},
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

func TestLookupFable5Output(t *testing.T) {
	if got := Lookup("claude-fable-5").Output; got != 50 {
		t.Errorf("Lookup(claude-fable-5).Output = %v, want 50", got)
	}
}

func TestLookupPrefixFallback(t *testing.T) {
	cases := []struct {
		name  string
		model string
		want  ModelPrices
	}{
		{"exact hit stays exact", "claude-fable-5", Table["claude-fable-5"]},
		{"dated opus 4.8 suffix", "claude-opus-4-8-20260301", Table["claude-opus-4-8"]},
		{"dated fable suffix", "claude-fable-5-20260520", Table["claude-fable-5"]},
		{"dated sonnet suffix", "claude-sonnet-4-6-20251114", Table["claude-sonnet-4-6"]},
		{"unknown model falls to Default", "claude-zephyr-9", Default},
		{"prefix without dash separator is not a match", "claude-fable-55", Default},
	}
	for _, c := range cases {
		if got := Lookup(c.model); got != c.want {
			t.Errorf("%s: Lookup(%q) = %+v, want %+v", c.name, c.model, got, c.want)
		}
	}
}

// TestSessionCostVariesByModel asserts pricing is behavioral: identical token
// counts must produce different USD costs for fable-5 vs sonnet-4-6 sessions.
func TestSessionCostVariesByModel(t *testing.T) {
	const inTok, cw, cr, outTok = 100_000, 200_000, 500_000, 50_000
	fable := SessionCostUSD(Lookup("claude-fable-5"), inTok, cw, cr, outTok)
	sonnet := SessionCostUSD(Lookup("claude-sonnet-4-6"), inTok, cw, cr, outTok)
	if fable == sonnet {
		t.Fatalf("fable and sonnet costs identical (%v) — pricing not varying with model", fable)
	}
	if fable <= sonnet {
		t.Errorf("fable cost %v should exceed sonnet cost %v", fable, sonnet)
	}
	// Fable must price at 10/50, not the 3/15 Default: 1M in + 1M out = $60.
	got := SessionCostUSD(Lookup("claude-fable-5"), 1_000_000, 0, 0, 1_000_000)
	const want, eps = 60.00, 1e-9
	if diff := got - want; diff < -eps || diff > eps {
		t.Errorf("fable 1M+1M = %.6f, want %.6f", got, want)
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
