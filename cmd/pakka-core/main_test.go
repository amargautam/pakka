package main

import "testing"

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
