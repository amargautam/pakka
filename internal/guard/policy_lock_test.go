package guard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/policy"
)

// TestGuardCategoriesAreValidPolicyCategories guards against drift: every guard
// bash heuristic pattern name must be a category a policy can lock, so
// policy.validCategories never silently omits a real guard category.
func TestGuardCategoriesAreValidPolicyCategories(t *testing.T) {
	for _, c := range bashChecks {
		if !policy.IsValidCategory(c.pattern) {
			t.Errorf("guard pattern %q is not a valid policy lockable category — add it to policy.validCategories", c.pattern)
		}
	}
}

func writeRepoPolicy(t *testing.T, repo, body string) {
	t.Helper()
	dir := filepath.Join(repo, ".pakka")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// AC3: a policy-locked category can never be overridden by the learned
// allowlist — a recorded override for that category is ignored, the block stands.
func TestPolicyLockedCategoryIgnoresOverride(t *testing.T) {
	repo := newRepo(t)
	cmd := "ls ../../sibling/dir" // traversal
	cfg := DefaultConfig()

	// Record an override that would normally allow this exact shape.
	r := RunWithConfig(bashEvent(cmd, repo), cfg)
	if err := RecordOverride(repo, r.Pattern, r.Shape, cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Sanity: without policy the override now allows it.
	if r2 := RunWithConfig(bashEvent(cmd, repo), cfg); !r2.Allowed {
		t.Fatalf("precondition: override should allow before policy lock: %s", r2.Reason)
	}

	// Lock the traversal category via policy.
	writeRepoPolicy(t, repo, `{"v":1,"lockedCategories":["traversal"]}`)

	r3 := RunWithConfig(bashEvent(cmd, repo), cfg)
	if r3.Allowed {
		t.Fatal("policy-locked category must stay blocked despite a recorded override")
	}
	if r3.Allowlistable {
		t.Fatal("locked category must not offer an override (ask)")
	}
	if r3.PolicyLocked != "traversal" {
		t.Fatalf("PolicyLocked = %q, want traversal", r3.PolicyLocked)
	}
}

// A non-locked category is unaffected by the policy.
func TestPolicyUnlockedCategoryStillOverridable(t *testing.T) {
	repo := newRepo(t)
	cmd := "ls ../../sibling/dir"
	cfg := DefaultConfig()
	writeRepoPolicy(t, repo, `{"v":1,"lockedCategories":["eval"]}`)

	r := RunWithConfig(bashEvent(cmd, repo), cfg)
	if !r.Allowlistable {
		t.Fatal("traversal must remain overridable when only eval is locked")
	}
	if r.PolicyLocked != "" {
		t.Fatalf("PolicyLocked should be empty for an unlocked category, got %q", r.PolicyLocked)
	}
}

// A malformed/too-new policy fails closed in guard: block stands, no override.
func TestPolicyErrorFailsClosedInGuard(t *testing.T) {
	repo := newRepo(t)
	cmd := "ls ../../sibling/dir"
	cfg := DefaultConfig()
	writeRepoPolicy(t, repo, `{"v":2}`)

	r := RunWithConfig(bashEvent(cmd, repo), cfg)
	if r.Allowed {
		t.Fatal("too-new policy must fail closed (block)")
	}
	if r.Allowlistable {
		t.Fatal("policy error must not offer an override")
	}
	if r.PolicyErr == "" {
		t.Fatal("PolicyErr must be set for a fail-closed policy error")
	}
}

// AC1: no policy file → guard override behavior is unchanged.
func TestGuardNoPolicyUnchanged(t *testing.T) {
	repo := newRepo(t)
	cmd := "ls ../../sibling/dir"
	cfg := DefaultConfig()
	if err := RecordOverride(repo, "traversal", Shape(cmd), cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := RunWithConfig(bashEvent(cmd, repo), cfg)
	if !r.Allowed {
		t.Fatalf("no-policy override must allow: %s", r.Reason)
	}
	if r.PolicyLocked != "" || r.PolicyErr != "" {
		t.Fatal("no-policy run must not set policy fields")
	}
}
