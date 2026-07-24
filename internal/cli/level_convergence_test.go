package cli

import (
	"testing"

	"github.com/amargautam/pakka/internal/compress/semantic"
)

// TestResolveOutputLevelConvergence is the issue #28 convergence guard for the
// package-main resolver. Every empty/invalid input must collapse to the SAME
// level that semantic.ParseLevel produces — the single source of truth — so the
// output vector never diverges from file-compression/orchestration intensity.
//
// Behavioral, not a hardcoded constant: the expected value is read from
// semantic.ParseLevel at runtime, so if either side drifts the test breaks.
func TestResolveOutputLevelConvergence(t *testing.T) {
	for _, in := range []string{"", "garbage", "audit", "fast", "ULTRA"} {
		want := string(semantic.ParseLevel(in))
		if got := resolveOutputLevel(in); got != want {
			t.Errorf("resolveOutputLevel(%q)=%q, must converge with semantic.ParseLevel=%q",
				in, got, want)
		}
	}
	// Empty input specifically must be the brand default super-ultra, NOT ultra.
	if resolveOutputLevel("") != string(semantic.LevelSuperUltra) {
		t.Errorf("resolveOutputLevel(\"\")=%q, want super-ultra", resolveOutputLevel(""))
	}
	if resolveOutputLevel("") == string(semantic.LevelUltra) {
		t.Errorf("resolveOutputLevel(\"\") wrongly resolved to legacy ultra")
	}
}

// TestSemanticDefaultLevelConvergence guards the compress_cmd.go entry point:
// when --semantic is set without --level, the level string that reaches
// semantic.ParseLevel must resolve to the brand default super-ultra — the same
// tier as resolveOutputLevel("") and ParseLevel(""). Before issue #28 this path
// hard-coded "ultra", diverging the output vector from every other vector.
func TestSemanticDefaultLevelConvergence(t *testing.T) {
	// semanticDefaultLevel is the extracted defaulting decision from runCompress.
	got := semantic.ParseLevel(semanticDefaultLevel(""))
	if got != semantic.LevelSuperUltra {
		t.Errorf("semantic default (empty --level) resolved to %q, want super-ultra", got)
	}
	if got == semantic.LevelUltra {
		t.Errorf("semantic default wrongly resolved to legacy ultra")
	}
	// Must match the other two entry points exactly.
	if string(got) != resolveOutputLevel("") {
		t.Errorf("semantic default %q diverges from resolveOutputLevel(\"\")=%q",
			got, resolveOutputLevel(""))
	}
	// An explicit level must still pass through untouched.
	if l := semanticDefaultLevel("lite"); l != "lite" {
		t.Errorf("explicit level must pass through: got %q", l)
	}
}
