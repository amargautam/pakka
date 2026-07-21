package statusline

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/hookevent"
)

// runNative invokes Run with a native CC 2.1 payload and returns the rendered
// string.
func runNative(t *testing.T, event *hookevent.Event, native *NativePayload, level string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Run(event, native, &buf, level, 0); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// nativeCtx builds a NativePayload with the supplied current-usage numbers and
// used_percentage.
func nativeCtx(in, out, cacheCreation, cacheRead, size int64, usedPct float64) *NativePayload {
	pct := usedPct
	return &NativePayload{
		ContextWindow: &ContextWindowInfo{
			ContextWindowSize: size,
			UsedPercentage:    &pct,
			CurrentUsage: &ContextUsage{
				InputTokens:              in,
				OutputTokens:             out,
				CacheCreationInputTokens: cacheCreation,
				CacheReadInputTokens:     cacheRead,
			},
		},
	}
}

// TestRunNativeContextSegment_TokensAndPercent — with a CC 2.1 payload the
// status line shows context-window usage as absolute tokens AND percent
// (never percent alone), and both values track the payload, not a constant.
//
// Absolute = input + cache_creation + cache_read (the docs' input-only
// formula backing used_percentage).
func TestRunNativeContextSegment_TokensAndPercent(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	event := &hookevent.Event{SessionID: "abc12345", CWD: "/work/x"}

	// 8500 + 5000 + 2000 = 15500 → "15.5K", pct 8.
	small := runNative(t, event, nativeCtx(8500, 1200, 5000, 2000, 200000, 8), "strict")
	// 100000 + 60000 + 50000 = 210000 → "210.0K", pct 42.
	large := runNative(t, event, nativeCtx(100000, 9000, 60000, 50000, 500000, 42), "strict")

	if !strings.Contains(small, "15.5K") {
		t.Errorf("small render missing absolute ctx tokens 15.5K: %q", small)
	}
	if !strings.Contains(small, "(8%)") {
		t.Errorf("small render missing ctx percent (8%%): %q", small)
	}
	if !strings.Contains(large, "210.0K") {
		t.Errorf("large render missing absolute ctx tokens 210.0K: %q", large)
	}
	if !strings.Contains(large, "(42%)") {
		t.Errorf("large render missing ctx percent (42%%): %q", large)
	}
	// Cross-leak guard: values must come from the payload, not a fixture echo.
	if strings.Contains(large, "15.5K") || strings.Contains(large, "(8%)") {
		t.Errorf("large render leaked small payload values: %q", large)
	}
	if strings.Contains(small, "210.0K") || strings.Contains(small, "(42%)") {
		t.Errorf("small render leaked large payload values: %q", small)
	}
	// Existing segments retained.
	for _, want := range []string{"pakka", "[strict]", "~$", "saved", "(est)", "bugs caught"} {
		if !strings.Contains(small, want) {
			t.Errorf("missing %q in %q", want, small)
		}
	}
}

// TestRunWithoutNative_NoContextSegment — pre-2.1 payload (nil native, or
// null current_usage) renders today's format: no ctx segment, no blank "()"
// artifacts, no crash.
func TestRunWithoutNative_NoContextSegment(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	event := &hookevent.Event{SessionID: "abc12345", CWD: "/work/x"}

	for name, native := range map[string]*NativePayload{
		"nil-native":         nil,
		"nil-context-window": {},
		"null-current-usage": {ContextWindow: &ContextWindowInfo{ContextWindowSize: 200000}},
	} {
		out := runNative(t, event, native, "strict")
		if strings.Contains(out, "ctx") {
			t.Errorf("%s: must not render ctx segment: %q", name, out)
		}
		if strings.Contains(out, "()") || strings.Contains(out, "(%)") {
			t.Errorf("%s: blank artifact in render: %q", name, out)
		}
		for _, want := range []string{"pakka", "[strict]", "~$", "saved", "(est)", "bugs caught"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q in %q", name, want, out)
			}
		}
	}
}

// extractDollar pulls the "~$X.XX" token out of a rendered run line, from the
// "~$" marker up to (excluding) " saved". Returns "" when absent.
func extractDollar(s string) string {
	i := strings.Index(s, "~$")
	if i < 0 {
		return ""
	}
	rest := s[i:]
	end := strings.Index(rest, " saved")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// TestRunNativeDollarRepoCumulative locks the #34 fix: the $ estimate is
// repo-cumulative (from the cached transcript scan), NEVER session-scoped from
// the native payload. The native payload feeds ONLY the ctx segment.
//
// Three behavioral assertions:
//  1. $ VARIES with transcript history (repo-cumulative source).
//  2. $ does NOT vary with the payload's session output tokens.
//  3. the ctx segment DOES vary with the payload.
func TestRunNativeDollarRepoCumulative(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A", "/repo/B": "/repo/B"})

	// Two repos with different cumulative output history.
	writeTranscriptDir(t, home, "-repo-A", "a.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 100_000},
	})
	writeTranscriptDir(t, home, "-repo-B", "b.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 500_000},
	})
	eventA := &hookevent.Event{SessionID: "sessA", CWD: "/repo/A"}
	eventB := &hookevent.Event{SessionID: "sessB", CWD: "/repo/B"}

	// Two payloads with very different session output tokens and ctx usage.
	payloadSmall := nativeCtx(1000, 2000, 0, 0, 200000, 1)
	payloadLarge := nativeCtx(50000, 200_000, 0, 0, 200000, 25)

	// (1) $ VARIES with transcript history — same payload, different repo history.
	dA := extractDollar(runNative(t, eventA, payloadSmall, "super-ultra"))
	dB := extractDollar(runNative(t, eventB, payloadSmall, "super-ultra"))
	if dA == "" || dB == "" {
		t.Fatalf("missing $ segment: A=%q B=%q", dA, dB)
	}
	if dA == dB {
		t.Errorf("$ must vary with transcript history: A=%q B=%q", dA, dB)
	}

	// (2) $ does NOT vary with payload session usage — same repo, different payload.
	dSmallPayload := extractDollar(runNative(t, eventA, payloadSmall, "super-ultra"))
	dLargePayload := extractDollar(runNative(t, eventA, payloadLarge, "super-ultra"))
	if dSmallPayload != dLargePayload {
		t.Errorf("$ must NOT vary with payload session usage: small=%q large=%q", dSmallPayload, dLargePayload)
	}

	// (3) ctx segment DOES vary with payload — 1.0K vs 50.0K tokens.
	renSmall := runNative(t, eventA, payloadSmall, "super-ultra")
	renLarge := runNative(t, eventA, payloadLarge, "super-ultra")
	if !strings.Contains(renSmall, "ctx 1.0K") {
		t.Errorf("small payload ctx segment want ctx 1.0K, got %q", renSmall)
	}
	if !strings.Contains(renLarge, "ctx 50.0K") {
		t.Errorf("large payload ctx segment want ctx 50.0K, got %q", renLarge)
	}
}

// --- CC 2.1 native statusLine payload parsing ---

// TestRunNativeUsesCumulativeScanForDollar — the #34 contract: even with a CC
// 2.1 native payload present, the $ estimate derives from the repo-cumulative
// transcript scan, never the session-scoped payload. Behavioral probes:
//  1. the transcript cache IS written (the scan runs) on a native render;
//  2. $ reflects the transcript fixture (~$4.9x) regardless of the payload's
//     output tokens, and two different payloads yield the SAME $;
//  3. a native render and a fallback (nil-native) render yield the same $.
func TestRunNativeUsesCumulativeScanForDollar(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})
	event := &hookevent.Event{SessionID: "abc12345", CWD: "/repo/A"}

	// Transcript fixture: 500K cumulative output → ~$4.9x saved on super-ultra.
	writeTranscriptDir(t, home, "-repo-A", "t.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 500_000},
	})
	// Pre-create ~/.pakka so the scan's cache save is not a silent no-op.
	if err := os.MkdirAll(filepath.Join(home, ".pakka"), 0700); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(home, ".pakka", "transcript-cache.json")

	// Two native payloads with very different session output tokens.
	small := runNative(t, event, nativeCtx(1000, 2000, 0, 0, 200000, 1), "super-ultra")
	large := runNative(t, event, nativeCtx(1000, 200_000, 0, 0, 200000, 1), "super-ultra")

	// (1) The scan runs → cache written even on a native render.
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("native render must use transcript scan (no cache at %s: %v)", cachePath, err)
	}
	// (2) $ reflects the transcript fixture, NOT the native output tokens.
	if !strings.Contains(small, "~$4.9") {
		t.Errorf("native render $ must derive from transcripts (~$4.9x), got %q", small)
	}
	if extractDollar(small) != extractDollar(large) {
		t.Errorf("$ must NOT vary with native payload output: small=%q large=%q", extractDollar(small), extractDollar(large))
	}
	// The session-scoped figure the old (buggy) path produced for 200K native
	// output was ~$1.98 — it must not appear.
	if strings.Contains(large, "~$1.98") {
		t.Errorf("session-scoped native output leaked into $: %q", large)
	}

	// (3) Fallback render (nil native) uses the same scan → same $.
	fallback := runNative(t, event, nil, "super-ultra")
	if extractDollar(fallback) != extractDollar(small) {
		t.Errorf("fallback and native $ must match (both cumulative): fallback=%q native=%q", extractDollar(fallback), extractDollar(small))
	}
}

// TestRunNativeNullUsedPercentage_DerivesFromWindowSize — docs: used_percentage
// may be null early in the session while current_usage is populated. The pct
// must then be derived from context_window_size — and vary with it — instead
// of dropping the segment or showing percent-less tokens.
func TestRunNativeNullUsedPercentage_DerivesFromWindowSize(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	useFakeHome(t, t.TempDir())
	useFakeRepoKey(t, nil)
	event := &hookevent.Event{SessionID: "abc12345", CWD: "/work/x"}

	mk := func(size int64) *NativePayload {
		return &NativePayload{ContextWindow: &ContextWindowInfo{
			ContextWindowSize: size,
			UsedPercentage:    nil,
			CurrentUsage:      &ContextUsage{InputTokens: 40000, CacheCreationInputTokens: 10000},
		}}
	}
	// 50000 / 200000 = 25%; 50000 / 500000 = 10%. Same tokens, different size.
	at200k := runNative(t, event, mk(200000), "strict")
	at500k := runNative(t, event, mk(500000), "strict")

	for _, out := range []string{at200k, at500k} {
		if !strings.Contains(out, "50.0K") {
			t.Errorf("missing absolute ctx tokens 50.0K: %q", out)
		}
	}
	if !strings.Contains(at200k, "(25%)") {
		t.Errorf("size 200000 should derive (25%%): %q", at200k)
	}
	if !strings.Contains(at500k, "(10%)") {
		t.Errorf("size 500000 should derive (10%%): %q", at500k)
	}

	// current_usage present but neither used_percentage nor window size:
	// tokens without percent would break the tokens-AND-percent rule — the
	// segment must be omitted, not rendered percent-less.
	noPct := runNative(t, event, &NativePayload{ContextWindow: &ContextWindowInfo{
		CurrentUsage: &ContextUsage{InputTokens: 40000},
	}}, "strict")
	if strings.Contains(noPct, "ctx") {
		t.Errorf("no pct derivable: ctx segment must be omitted, got %q", noPct)
	}
}

// TestParseNativePayload_VariesWithInput — behavioral: two different payloads
// must produce two different parsed values, and the parsed numbers must match
// the JSON, not a fixed shape.
func TestParseNativePayload_VariesWithInput(t *testing.T) {
	a := ParseNativePayload([]byte(`{
		"cost": {"total_cost_usd": 0.25},
		"context_window": {
			"context_window_size": 200000,
			"used_percentage": 8,
			"current_usage": {
				"input_tokens": 8500,
				"output_tokens": 1200,
				"cache_creation_input_tokens": 5000,
				"cache_read_input_tokens": 2000
			}
		}
	}`))
	b := ParseNativePayload([]byte(`{
		"cost": {"total_cost_usd": 1.75},
		"context_window": {
			"context_window_size": 500000,
			"used_percentage": 42,
			"current_usage": {
				"input_tokens": 100000,
				"output_tokens": 9000,
				"cache_creation_input_tokens": 60000,
				"cache_read_input_tokens": 50000
			}
		}
	}`))

	if a == nil || b == nil {
		t.Fatalf("valid payloads must parse non-nil: a=%v b=%v", a, b)
	}
	if a.ContextWindow == nil || a.ContextWindow.CurrentUsage == nil {
		t.Fatalf("payload a: context_window/current_usage must be present: %+v", a)
	}
	if got := a.ContextWindow.CurrentUsage.InputTokens; got != 8500 {
		t.Errorf("a input_tokens = %d, want 8500", got)
	}
	if got := b.ContextWindow.CurrentUsage.InputTokens; got != 100000 {
		t.Errorf("b input_tokens = %d, want 100000", got)
	}
	if a.ContextWindow.CurrentUsage.InputTokens == b.ContextWindow.CurrentUsage.InputTokens {
		t.Error("parsed input_tokens must vary with payload")
	}
	if a.ContextWindow.ContextWindowSize == b.ContextWindow.ContextWindowSize {
		t.Error("parsed context_window_size must vary with payload")
	}
	if a.ContextWindow.UsedPercentage == nil || *a.ContextWindow.UsedPercentage != 8 {
		t.Errorf("a used_percentage = %v, want 8", a.ContextWindow.UsedPercentage)
	}
	if a.Cost == nil || a.Cost.TotalCostUSD != 0.25 {
		t.Errorf("a total_cost_usd = %+v, want 0.25", a.Cost)
	}
	if b.Cost == nil || b.Cost.TotalCostUSD != 1.75 {
		t.Errorf("b total_cost_usd = %+v, want 1.75", b.Cost)
	}
}

// TestParseNativePayload_AbsentAndNullFields — older CC payloads (no cost, no
// context_window) and the documented null states must not synthesize data.
func TestParseNativePayload_AbsentAndNullFields(t *testing.T) {
	// Pre-2.1 shape: hook-event fields only.
	old := ParseNativePayload([]byte(`{"session_id":"s1","cwd":"/work/x","transcript_path":"/t.jsonl"}`))
	if old == nil {
		t.Fatal("pre-2.1 payload must still parse non-nil")
	}
	if old.ContextWindow != nil {
		t.Errorf("absent context_window must parse as nil, got %+v", old.ContextWindow)
	}
	if old.Cost != nil {
		t.Errorf("absent cost must parse as nil, got %+v", old.Cost)
	}

	// Documented null states: current_usage/used_percentage null before first
	// API call and right after /compact.
	nulls := ParseNativePayload([]byte(`{
		"context_window": {
			"context_window_size": 200000,
			"used_percentage": null,
			"current_usage": null
		}
	}`))
	if nulls == nil || nulls.ContextWindow == nil {
		t.Fatalf("context_window with null members must parse: %+v", nulls)
	}
	if nulls.ContextWindow.CurrentUsage != nil {
		t.Errorf("null current_usage must parse as nil, got %+v", nulls.ContextWindow.CurrentUsage)
	}
	if nulls.ContextWindow.UsedPercentage != nil {
		t.Errorf("null used_percentage must parse as nil, got %v", *nulls.ContextWindow.UsedPercentage)
	}

	// Malformed input: nil, never panic.
	if got := ParseNativePayload([]byte(`{not json`)); got != nil {
		t.Errorf("malformed payload must parse as nil, got %+v", got)
	}
	if got := ParseNativePayload(nil); got != nil {
		t.Errorf("nil input must parse as nil, got %+v", got)
	}
}
