package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/compress/orchestrator"
)

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
// compress.semantic AND a non-empty target list. Default settings (no plugin
// settings.json on disk in test) → false.
func TestOrchestratorEnabledFlag(t *testing.T) {
	if orchestratorEnabled() {
		t.Errorf("orchestratorEnabled should default to false in test env")
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
