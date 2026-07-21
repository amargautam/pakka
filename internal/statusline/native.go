package statusline

import (
	"encoding/json"
	"math"
)

// NativePayload holds the Claude Code 2.1+ statusLine stdin fields that pakka
// consumes natively instead of deriving from transcript scans.
//
// Docs: https://code.claude.com/docs/en/statusline.md (fetched 2026-07-21).
// Absence semantics per docs:
//   - `cost` / `context_window`: absent on pre-2.1 Claude Code — nil here.
//   - `context_window.current_usage`: null before the first API call and
//     immediately after /compact — nil here.
//   - `context_window.used_percentage`: may be null early in session — nil here.
//
// Callers must treat nil at any level as "native data unavailable" and fall
// back to the legacy transcript-scan path.
type NativePayload struct {
	Cost          *CostInfo          `json:"cost"`
	ContextWindow *ContextWindowInfo `json:"context_window"`
}

// CostInfo mirrors the statusLine `cost` object (subset pakka uses).
type CostInfo struct {
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// ContextWindowInfo mirrors the statusLine `context_window` object.
type ContextWindowInfo struct {
	ContextWindowSize int64         `json:"context_window_size"`
	UsedPercentage    *float64      `json:"used_percentage"`
	CurrentUsage      *ContextUsage `json:"current_usage"`
}

// ContextUsage mirrors `context_window.current_usage` — the live context
// window token counts from the most recent API response.
type ContextUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// nativeContextUsage extracts the context-usage segment values from a native
// payload. Returns ok=false when the payload (or any required member) is
// absent or null — the caller then omits the segment.
//
// tokens follows the docs' input-only formula backing used_percentage:
// input_tokens + cache_creation_input_tokens + cache_read_input_tokens.
// pct prefers the native used_percentage; when that is null (early session)
// it is derived from context_window_size. Neither derivable → ok=false, so a
// percent is never shown without tokens and vice versa.
func nativeContextUsage(p *NativePayload) (ok bool, tokens, pct int64) {
	if p == nil || p.ContextWindow == nil || p.ContextWindow.CurrentUsage == nil {
		return false, 0, 0
	}
	cw := p.ContextWindow
	u := cw.CurrentUsage
	tokens = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	switch {
	case cw.UsedPercentage != nil:
		pct = int64(math.Round(*cw.UsedPercentage))
	case cw.ContextWindowSize > 0:
		pct = pctRound(tokens, cw.ContextWindowSize)
	default:
		return false, 0, 0
	}
	return true, tokens, pct
}

// ParseNativePayload parses the raw statusLine stdin JSON for CC 2.1 native
// fields. Returns nil on empty or malformed input — never panics. Absent or
// null fields stay nil so fallback detection is a simple nil check.
func ParseNativePayload(data []byte) *NativePayload {
	if len(data) == 0 {
		return nil
	}
	var p NativePayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil
	}
	return &p
}
