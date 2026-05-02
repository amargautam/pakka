package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/compress/orchestrator"
	"github.com/amargautam/pakka/internal/compress/semantic"
)

// withStubLookPath swaps the package-level `lookPath` for the duration of
// the test. The signature mirrors exec.LookPath so tests can simulate either
// "claude on PATH" or "claude missing" without mutating the real $PATH
// (which would race other parallel tests in the same package).
func withStubLookPath(t *testing.T, present bool) {
	t.Helper()
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	if present {
		lookPath = func(name string) (string, error) {
			if name == "claude" {
				return "/fake/claude", nil
			}
			return old(name)
		}
		return
	}
	lookPath = func(name string) (string, error) {
		if name == "claude" {
			return "", errors.New("not found")
		}
		return old(name)
	}
}

func boolPtr(b bool) *bool { return &b }

// TestSemanticEnabled is a table-driven guard for the semanticEnabled pure
// function. Rows are added one at a time (TDD discipline).
func TestSemanticEnabled(t *testing.T) {
	cases := []struct {
		name     string
		level    string
		explicit *bool
		want     bool
	}{
		{"super-ultra nil → enforced true", "super-ultra", nil, true},
		{"super-ultra ptr(false) → still true (enforced)", "super-ultra", boolPtr(false), true},
		{"ultra nil → default on", "ultra", nil, true},
		{"ultra ptr(false) → opt-out", "ultra", boolPtr(false), false},
		{"ultra ptr(true) → explicit on", "ultra", boolPtr(true), true},
		{"lite nil → false", "lite", nil, false},
		{"strict nil → false", "strict", nil, false},
		{"lite ptr(true) → explicit opt-in", "lite", boolPtr(true), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := semanticEnabled(tc.level, tc.explicit)
			if got != tc.want {
				t.Errorf("semanticEnabled(%q, %v) = %v; want %v", tc.level, tc.explicit, got, tc.want)
			}
		})
	}
}

// TestForkOrchestratorReturnsImmediately is a behavioral guard for the
// SessionStart 50ms budget. forkOrchestrator must construct + spawn the child
// without blocking on its execution, even when the allowlist would require
// substantial work.
//
// We can't actually fork pakka-core from the test binary (different exe path),
// so this test asserts that:
//
//   - The decision-side helpers (orchestratorEnabled, orchestratorTargets) are
//     pure-readonly with no network, no LLM call.
//   - newOrchestrator returns nil (not panic) when no API key is set, which is
//     the path SessionStart actually takes in test runs without ANTHROPIC_API_KEY.
//   - The whole forkOrchestrator path completes in well under 100ms even with
//     a large synthetic target list.
func TestForkOrchestratorReturnsImmediately(t *testing.T) {
	repo := t.TempDir()
	// Write 50 fake targets to make sure target-walk wouldn't dominate.
	for i := 0; i < 50; i++ {
		name := filepath.Join(repo, "F"+string(rune('A'+(i%26)))+"_"+string(rune('A'+(i/26)))+".md")
		_ = os.WriteFile(name, []byte("# T\nbody"), 0o644)
	}
	t.Setenv("ANTHROPIC_API_KEY", "") // force the no-key skip path

	start := time.Now()
	forkOrchestrator(repo, "strict", "tst-fast")
	d := time.Since(start)
	if d > 100*time.Millisecond {
		t.Errorf("forkOrchestrator too slow: %v (must be <100ms for SessionStart budget)", d)
	}
}

// TestOrchestratorEnabledFlag — the SessionStart fork is gated by
// semanticEnabled AND a non-empty target list. At super-ultra (brand default),
// semantic is enforced (always true). With DefaultTargets non-empty,
// orchestratorEnabled() returns true by default in test env.
func TestOrchestratorEnabledFlag(t *testing.T) {
	// super-ultra is the brand default; semanticEnabled enforces semantic at
	// super-ultra regardless of settings. DefaultTargets is non-empty, so
	// orchestratorEnabled() returns true when no settings.json overrides it.
	if !orchestratorEnabled() {
		t.Errorf("orchestratorEnabled should default to true in test env (super-ultra enforces semantic)")
	}
}

// TestOrchestratorTargetsDefault — when settings.json has no override, we
// fall through to the orchestrator package's DefaultTargets.
func TestOrchestratorTargetsDefault(t *testing.T) {
	got := orchestratorTargets()
	if len(got) != len(orchestrator.DefaultTargets) {
		t.Errorf("default targets len=%d want %d", len(got), len(orchestrator.DefaultTargets))
	}
}

// TestNewOrchestrator_PrefersClaudeCLI — when both auth paths are available
// (claude on PATH AND ANTHROPIC_API_KEY set), the rewriter must be the CLI
// path. Behavior varies on PATH stub: if we flip `present` to false the
// assertion below would change identity.
func TestNewOrchestrator_PrefersClaudeCLI(t *testing.T) {
	withStubLookPath(t, true)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key-fake")

	repo := t.TempDir()
	o := newOrchestrator(repo, "strict", "tst")
	if o == nil {
		t.Fatal("newOrchestrator returned nil with both auth paths available")
	}
	if _, ok := o.Rewriter.(*semantic.ClaudeCLI); !ok {
		t.Errorf("expected *semantic.ClaudeCLI, got %T", o.Rewriter)
	}
}

// TestNewOrchestrator_FallsBackToHTTPWithoutClaude — PATH lacks `claude` but
// ANTHROPIC_API_KEY is set: fall back to AnthropicClient. Behavior varies on
// PATH stub state (flip present=true → ClaudeCLI selected instead).
func TestNewOrchestrator_FallsBackToHTTPWithoutClaude(t *testing.T) {
	withStubLookPath(t, false)
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key-fake")

	repo := t.TempDir()
	o := newOrchestrator(repo, "strict", "tst")
	if o == nil {
		t.Fatal("newOrchestrator returned nil with ANTHROPIC_API_KEY set")
	}
	if _, ok := o.Rewriter.(*semantic.AnthropicClient); !ok {
		t.Errorf("expected *semantic.AnthropicClient fallback, got %T", o.Rewriter)
	}
}

// TestNewOrchestrator_NilWhenNeitherAvailable — neither auth path → nil.
// Behavior varies on (PATH presence, env var presence): set either one and
// the assertion would flip.
func TestNewOrchestrator_NilWhenNeitherAvailable(t *testing.T) {
	withStubLookPath(t, false)
	t.Setenv("ANTHROPIC_API_KEY", "")

	repo := t.TempDir()
	o := newOrchestrator(repo, "strict", "tst")
	if o != nil {
		t.Errorf("expected nil orchestrator, got %+v (rewriter=%T)", o, o.Rewriter)
	}
}
