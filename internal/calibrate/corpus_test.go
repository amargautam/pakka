package calibrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCorpusPatchesApply is a regression guard for the seed corpus: every
// benchmarks/seeds/<seed>/seed.patch must apply cleanly through the exact
// production path (materializeSeed → temp git repo, git apply, git add). A
// malformed patch (wrong @@ line count, truncated hunk, CRLF, missing trailing
// newline) makes calibrate fail that seed with git apply exit 128, silently
// distorting precision/recall. This test fails loudly instead.
//
// Skips gracefully if git is absent or the corpus is not checked out (so it is
// a no-op in environments without benchmarks/seeds).
func TestCorpusPatchesApply(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	// Test cwd is the package dir (internal/calibrate); the corpus lives at
	// the repo root under benchmarks/seeds.
	seedsDir := filepath.Join("..", "..", "benchmarks", "seeds")
	entries, err := os.ReadDir(seedsDir)
	if err != nil {
		t.Skipf("seed corpus not present (%s): %v", seedsDir, err)
	}

	seeds := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		seedDir := filepath.Join(seedsDir, e.Name())
		if _, err := os.Stat(filepath.Join(seedDir, "seed.patch")); err != nil {
			continue // not a seed dir
		}
		seeds++
		t.Run(e.Name(), func(t *testing.T) {
			repo, cleanup, err := materializeSeed(seedDir)
			if err != nil {
				t.Fatalf("materializeSeed(%s) failed — patch does not apply cleanly: %v", e.Name(), err)
			}
			defer cleanup()
			// The seed adds a new file; it must show up staged.
			diff, err := runGit(repo, "diff", "--cached", "--name-only")
			if err != nil {
				t.Fatalf("git diff --cached: %v", err)
			}
			if len(diff) == 0 {
				t.Errorf("seed %s applied but staged no changes", e.Name())
			}
		})
	}

	if seeds == 0 {
		t.Skipf("no seed patches found under %s", seedsDir)
	}
}
