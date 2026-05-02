package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestEmitCommitRewrite_HookSpecificOutputShape is the Pass 4.7 regression
// guard for the PreToolUse hook contract. Claude Code expects:
//
//	{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"..."}}}
//
// Pre-Pass-4.7 pakka emitted the legacy `{"tool_input":{"command":"..."}}`
// shape; Claude Code silently ignored it and the rewritten command never
// reached git. Result: zero trailers across the entire commit history
// despite the gate running and writing "passed" verdicts.
//
// This test asserts the envelope at the JSON-byte level. A future edit that
// reverts to the legacy shape will fail loudly.
func TestEmitCommitRewrite_HookSpecificOutputShape(t *testing.T) {
	rewritten := "git commit -m 'msg' --trailer 'Reviewed-by-pakka: 92'"
	raw := emitCommitRewrite(rewritten)

	// Envelope must end with a newline so multiple hook outputs concat cleanly.
	if !strings.HasSuffix(string(raw), "\n") {
		t.Errorf("emitCommitRewrite output must end with newline; got %q", string(raw))
	}

	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("emitCommitRewrite produced invalid JSON: %v", err)
	}

	// Negative guard: the legacy `tool_input` key MUST NOT be present.
	// This is what Claude Code silently ignored.
	if _, has := got["tool_input"]; has {
		t.Errorf("legacy tool_input key present — pre-Pass-4.7 shape regressed: %s", string(raw))
	}

	hso, ok := got["hookSpecificOutput"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing hookSpecificOutput envelope: %s", string(raw))
	}

	if name, _ := hso["hookEventName"].(string); name != "PreToolUse" {
		t.Errorf("hookEventName = %q; want %q", name, "PreToolUse")
	}

	updated, ok := hso["updatedInput"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing hookSpecificOutput.updatedInput: %s", string(raw))
	}

	if cmd, _ := updated["command"].(string); cmd != rewritten {
		t.Errorf("updatedInput.command = %q; want %q", cmd, rewritten)
	}
}

// TestEmitCommitRewrite_VariesWithInput is a behavioral guard that the
// envelope's command field actually reflects the argument. Catches the
// class of bug Pass 4.1/5a/5b kept hitting: placeholder values shipping
// as if measured. Two different inputs must produce two different outputs.
func TestEmitCommitRewrite_VariesWithInput(t *testing.T) {
	a := emitCommitRewrite("git commit -m 'A'")
	b := emitCommitRewrite("git commit -m 'B'")
	if string(a) == string(b) {
		t.Errorf("emitCommitRewrite output did not vary with input — command field is a constant")
	}
	if !strings.Contains(string(a), "'A'") {
		t.Errorf("output A missing input marker: %s", string(a))
	}
	if !strings.Contains(string(b), "'B'") {
		t.Errorf("output B missing input marker: %s", string(b))
	}
}

// TestResolveOutputLevelDefaultSuperUltra asserts the brand default has been
// changed to super-ultra. When resolveOutputLevel("") returns "ultra" this
// test will be RED — which is the expected state before Step 2.
func TestResolveOutputLevelDefaultSuperUltra(t *testing.T) {
	got := resolveOutputLevel("")
	if got != "super-ultra" {
		t.Errorf("resolveOutputLevel(\"\") = %q; want %q (brand default changed to super-ultra)", got, "super-ultra")
	}
}

// TestLoadOutputLevel_DefaultsToSuperUltra is the brand-default regression
// guard. Empty settings, missing-field settings, and garbage values must all
// collapse to "super-ultra" — pakka's brand thesis is fewer tokens, and the
// default reflects it. A future edit that silently reintroduces "ultra" or
// "strict" as the default will fail this test loudly.
//
// See memory/DECISIONS.md "Default output level: super-ultra".
func TestLoadOutputLevel_DefaultsToSuperUltra(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty string (settings field absent / never set)", ""},
		{"unknown garbage value", "garbage"},
		{"legacy engine-mode leaking into output level", "audit"},
		{"removed legacy tier", "fast"},
		{"case-sensitive — legal lowercased only", "Strict"},
		{"case-sensitive — legal lowercased only", "ULTRA"},
		{"whitespace not normalized", " ultra"},
		{"whitespace not normalized", "ultra "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveOutputLevel(tc.raw)
			if got != "super-ultra" {
				t.Errorf("resolveOutputLevel(%q) = %q; want %q (brand default — see DECISIONS.md)",
					tc.raw, got, "super-ultra")
			}
			// Negative guard: legacy defaults must not leak through.
			if got == "strict" && tc.raw != "strict" {
				t.Errorf("resolveOutputLevel(%q) returned legacy default %q; brand default is now super-ultra",
					tc.raw, got)
			}
			if got == "ultra" && tc.raw != "ultra" {
				t.Errorf("resolveOutputLevel(%q) returned stale default %q; brand default is now super-ultra",
					tc.raw, got)
			}
		})
	}
}

// TestLoadOutputLevel_LegalValuesPassThrough verifies the four legal levels
// round-trip unchanged. Pass 4.4 changed the *default*; it did NOT remove
// any tier. `strict` is still selectable — a user who explicitly sets
// `pakka.compress.outputLevel: "strict"` in settings.json gets strict.
func TestLoadOutputLevel_LegalValuesPassThrough(t *testing.T) {
	for _, level := range []string{"lite", "strict", "ultra", "super-ultra"} {
		t.Run(level, func(t *testing.T) {
			got := resolveOutputLevel(level)
			if got != level {
				t.Errorf("resolveOutputLevel(%q) = %q; legal values must pass through unchanged",
					level, got)
			}
		})
	}
}

// TestLoadOutputLevel_DefaultDiffersFromAllNonDefaults — behavioral guard
// that varies-with-input. The default branch and every legal-value branch
// must produce distinct outputs from each other when the inputs are
// distinct. Catches a regression that collapses everything to a single
// constant (a class of bug Pass 4.1/5a/5b kept hitting — placeholder
// values shipped as if measured).
func TestLoadOutputLevel_DefaultDiffersFromAllNonDefaults(t *testing.T) {
	def := resolveOutputLevel("") // empty → default
	if def != "super-ultra" {
		t.Fatalf("empty-input default = %q, expected %q", def, "super-ultra")
	}
	// "lite", "strict", and "ultra" must produce outputs that differ from the default.
	for _, other := range []string{"lite", "strict", "ultra"} {
		got := resolveOutputLevel(other)
		if got == def {
			t.Errorf("resolveOutputLevel(%q) = %q == default; legal-value branch is collapsing",
				other, got)
		}
	}
}

// --- cycle 1: parseStrict (hard-fail, used by guard and commit-gate) ---

// TestParseStrictMalformedJSON_ReturnsError — RED until parseStrict is added.
// Guard/commit-gate must surface parse errors as stderr message + false return.
func TestParseStrictMalformedJSON_ReturnsError(t *testing.T) {
	var errBuf bytes.Buffer
	event, ok := parseStrict(strings.NewReader("{not json"), &errBuf)
	if ok {
		t.Fatal("parseStrict with malformed JSON: want ok=false, got ok=true")
	}
	if event != nil {
		t.Errorf("parseStrict malformed: want nil event, got %+v", event)
	}
	if !strings.Contains(errBuf.String(), "pakka: malformed hook event") {
		t.Errorf("parseStrict malformed: stderr should mention 'pakka: malformed hook event', got %q", errBuf.String())
	}
}

// TestParseStrictEmptyInput_SilentSkip — empty stdin means the hook is not
// being invoked for a hook event (e.g. skill direct call). Silent skip: ok=true, event=nil.
func TestParseStrictEmptyInput_SilentSkip(t *testing.T) {
	var errBuf bytes.Buffer
	event, ok := parseStrict(strings.NewReader(""), &errBuf)
	if !ok {
		t.Fatalf("parseStrict empty: want ok=true (silent skip), got ok=false; stderr=%q", errBuf.String())
	}
	if event != nil {
		t.Errorf("parseStrict empty: want nil event, got %+v", event)
	}
	if errBuf.Len() != 0 {
		t.Errorf("parseStrict empty: want no stderr, got %q", errBuf.String())
	}
}

// TestParseStrictValidJSON_ReturnsEvent — happy path.
func TestParseStrictValidJSON_ReturnsEvent(t *testing.T) {
	var errBuf bytes.Buffer
	input := `{"session_id":"abc123","hook_event_name":"PreToolUse","tool_name":"Bash"}`
	event, ok := parseStrict(strings.NewReader(input), &errBuf)
	if !ok {
		t.Fatalf("parseStrict valid: want ok=true, got false; stderr=%q", errBuf.String())
	}
	if event == nil {
		t.Fatal("parseStrict valid: want non-nil event, got nil")
	}
	if event.SessionID != "abc123" {
		t.Errorf("parseStrict: session_id=%q want %q", event.SessionID, "abc123")
	}
	if event.ToolName != "Bash" {
		t.Errorf("parseStrict: tool_name=%q want %q", event.ToolName, "Bash")
	}
}

// TestParseStrictVariesWithInput — behavioral guard: two different inputs must
// produce distinguishably different results.
func TestParseStrictVariesWithInput(t *testing.T) {
	var e1Buf, e2Buf bytes.Buffer
	e1, ok1 := parseStrict(strings.NewReader(`{"session_id":"A","tool_name":"Read"}`), &e1Buf)
	e2, ok2 := parseStrict(strings.NewReader(`{"session_id":"B","tool_name":"Bash"}`), &e2Buf)
	if !ok1 || !ok2 {
		t.Fatalf("parseStrict vary: both valid inputs must succeed (ok1=%v ok2=%v)", ok1, ok2)
	}
	if e1.SessionID == e2.SessionID {
		t.Errorf("parseStrict vary: session_id must differ; both=%q", e1.SessionID)
	}
	if e1.ToolName == e2.ToolName {
		t.Errorf("parseStrict vary: tool_name must differ; both=%q", e1.ToolName)
	}
}

// --- cycle 2: parseLenient (silent fallback, used by meter/audit/statusline/compress) ---

// TestParseLenientMalformedJSON_FallbackEvent — silent callers must get a
// fallback event (not nil) with a generated SessionID on bad JSON.
func TestParseLenientMalformedJSON_FallbackEvent(t *testing.T) {
	event := parseLenient(strings.NewReader("{not json"))
	if event == nil {
		t.Fatal("parseLenient malformed: want non-nil fallback event, got nil")
	}
	if event.SessionID == "" {
		t.Error("parseLenient malformed: SessionID should be non-empty (fallback)")
	}
	if !strings.HasPrefix(event.SessionID, "sess-") {
		t.Errorf("parseLenient malformed: fallback SessionID should start with sess-, got %q", event.SessionID)
	}
}

// TestParseLenientEmptyInput_FallbackEvent — empty stdin must still return
// a usable fallback event (not nil).
func TestParseLenientEmptyInput_FallbackEvent(t *testing.T) {
	event := parseLenient(strings.NewReader(""))
	if event == nil {
		t.Fatal("parseLenient empty: want non-nil fallback event, got nil")
	}
	if event.SessionID == "" {
		t.Error("parseLenient empty: SessionID should be non-empty (fallback)")
	}
}

// TestParseLenientValidJSON_ReturnsEvent — happy path, field values preserved.
func TestParseLenientValidJSON_ReturnsEvent(t *testing.T) {
	input := `{"session_id":"sid99","hook_event_name":"Stop","cwd":"/work/proj"}`
	event := parseLenient(strings.NewReader(input))
	if event == nil {
		t.Fatal("parseLenient valid: want non-nil event, got nil")
	}
	if event.SessionID != "sid99" {
		t.Errorf("parseLenient: session_id=%q want %q", event.SessionID, "sid99")
	}
	if event.CWD != "/work/proj" {
		t.Errorf("parseLenient: cwd=%q want %q", event.CWD, "/work/proj")
	}
}

// TestParseLenientVariesWithInput — behavioral guard.
func TestParseLenientVariesWithInput(t *testing.T) {
	e1 := parseLenient(strings.NewReader(`{"session_id":"X","cwd":"/a"}`))
	e2 := parseLenient(strings.NewReader(`{"session_id":"Y","cwd":"/b"}`))
	if e1 == nil || e2 == nil {
		t.Fatal("parseLenient vary: both must return non-nil")
	}
	if e1.SessionID == e2.SessionID {
		t.Errorf("parseLenient vary: session_id must differ; both=%q", e1.SessionID)
	}
	if e1.CWD == e2.CWD {
		t.Errorf("parseLenient vary: cwd must differ; e1=%q e2=%q", e1.CWD, e2.CWD)
	}
}
