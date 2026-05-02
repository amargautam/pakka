// Package pricing provides per-model token pricing and USD cost helpers.
//
// Source: https://platform.claude.com/docs/en/about-claude/pricing (fetched 2026-05-02)
package pricing

import "fmt"

// ModelPrices holds per-token rates for one model (USD per million tokens).
type ModelPrices struct {
	Input      float64 // base input
	Output     float64 // output
	CacheWrite float64 // 5-min cache write (1.25× input)
	CacheRead  float64 // cache read hit (0.1× input)
}

// Table maps model ID strings to their prices.
// Source: https://platform.claude.com/docs/en/about-claude/pricing (fetched 2026-05-02)
var Table = map[string]ModelPrices{
	"claude-opus-4-7":           {5.00, 25.00, 6.25, 0.50},
	"claude-opus-4-6":           {5.00, 25.00, 6.25, 0.50},
	"claude-opus-4-5":           {5.00, 25.00, 6.25, 0.50},
	"claude-sonnet-4-6":         {3.00, 15.00, 3.75, 0.30},
	"claude-sonnet-4-5":         {3.00, 15.00, 3.75, 0.30},
	"claude-haiku-4-5-20251001": {1.00, 5.00, 1.25, 0.10},
	"claude-haiku-3-5-20241022": {0.80, 4.00, 1.00, 0.08},
}

// Default is used when the model is unknown.
// Defaults to Sonnet 4.6 pricing.
var Default = ModelPrices{3.00, 15.00, 3.75, 0.30}

// Lookup returns the prices for the given model ID, falling back to Default.
func Lookup(model string) ModelPrices {
	if p, ok := Table[model]; ok {
		return p
	}
	return Default
}

// SessionCostUSD computes the USD cost of one API call given token counts.
// All token counts are in absolute tokens; rates are per million tokens.
func SessionCostUSD(p ModelPrices, inputTok, cacheWrite, cacheRead, outputTok int64) float64 {
	return (float64(inputTok)*p.Input +
		float64(cacheWrite)*p.CacheWrite +
		float64(cacheRead)*p.CacheRead +
		float64(outputTok)*p.Output) / 1_000_000
}

// FormatUSD formats a dollar amount: "$0.03", "$1.24", "$12.50".
func FormatUSD(dollars float64) string {
	if dollars < 0 {
		dollars = 0
	}
	return fmt.Sprintf("$%.2f", dollars)
}
