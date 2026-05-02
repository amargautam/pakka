package main

import (
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

// TestLoadOutputLevel_DefaultsToUltra is the Pass 4.4 regression guard for
// the brand default. Empty settings, missing-field settings, and garbage
// values must all collapse to "ultra" — pakka's brand thesis is fewer
// tokens, and the default reflects it. A future edit that re-introduces
// "strict" as the silent default will fail this test loudly.
//
// See memory/DECISIONS.md "Default output level: ultra (decided 2026-04-29)".
func TestLoadOutputLevel_DefaultsToUltra(t *testing.T) {
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
			if got != "ultra" {
				t.Errorf("resolveOutputLevel(%q) = %q; want %q (brand default — see DECISIONS.md)",
					tc.raw, got, "ultra")
			}
			// Negative guard: the legacy default must not leak through.
			if got == "strict" && tc.raw != "strict" {
				t.Errorf("resolveOutputLevel(%q) returned legacy default %q; Pass 4.4 flipped it to ultra",
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
	if def != "ultra" {
		t.Fatalf("empty-input default = %q, expected %q", def, "ultra")
	}
	// "lite" and "strict" must produce outputs that differ from the default.
	for _, other := range []string{"lite", "strict"} {
		got := resolveOutputLevel(other)
		if got == def {
			t.Errorf("resolveOutputLevel(%q) = %q == default; legal-value branch is collapsing",
				other, got)
		}
	}
}
