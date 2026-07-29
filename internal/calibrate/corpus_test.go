package calibrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestCorpusLineApproxLocatesBug guards the ground-truth line numbers. calibrate
// scores recall with a ±5 line matcher (see matchesExpected): a finding matches
// by (file basename AND line within ±5 of line_approx). If expected.json's
// line_approx points outside the patched file — or into blank space away from
// the planted bug — the matcher can never hit, so a correct reviewer is scored
// as a miss and recall is understated. This test materializes each bug/perf seed
// and asserts, mechanically, that line_approx lands inside the patched file and
// that the ±5 window it drives contains real (non-blank) code.
//
// Kept deliberately mechanical: no per-seed keyword heuristics. It verifies the
// number is well-formed against the source, not that it names the exact bug.
func TestCorpusLineApproxLocatesBug(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	const window = 5 // must match the ±5 in matchesExpected

	seedsDir := filepath.Join("..", "..", "benchmarks", "seeds")
	entries, err := os.ReadDir(seedsDir)
	if err != nil {
		t.Skipf("seed corpus not present (%s): %v", seedsDir, err)
	}

	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		seedDir := filepath.Join(seedsDir, e.Name())
		if _, err := os.Stat(filepath.Join(seedDir, "expected.json")); err != nil {
			continue // not a seed dir
		}

		exp, _, err := loadExpected(seedDir)
		if err != nil {
			t.Errorf("%s: loadExpected: %v", e.Name(), err)
			continue
		}
		if exp.IsClean() {
			continue // clean fixtures plant no bug and carry no line_approx
		}
		checked++

		t.Run(e.Name(), func(t *testing.T) {
			if exp.File == "" {
				t.Fatalf("%s: expected.json has empty file", e.Name())
			}
			if exp.LineApprox < 1 {
				t.Fatalf("%s: line_approx %d must be >= 1", e.Name(), exp.LineApprox)
			}

			repo, cleanup, err := materializeSeed(seedDir)
			if err != nil {
				t.Fatalf("materializeSeed(%s): %v", e.Name(), err)
			}
			defer cleanup()

			// expected.json's file path must exist in the patched tree.
			src := filepath.Join(repo, filepath.FromSlash(exp.File))
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatalf("%s: expected file %q not in patched tree: %v", e.Name(), exp.File, err)
			}
			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			n := len(lines)

			// line_approx must fall inside the file.
			if exp.LineApprox > n {
				t.Fatalf("%s: line_approx %d exceeds %s line count %d", e.Name(), exp.LineApprox, exp.File, n)
			}

			// The ±window the matcher uses must contain at least one non-blank
			// code line — otherwise a correct finding cannot score.
			lo := exp.LineApprox - window
			if lo < 1 {
				lo = 1
			}
			hi := exp.LineApprox + window
			if hi > n {
				hi = n
			}
			nonBlank := false
			for i := lo; i <= hi; i++ {
				if strings.TrimSpace(lines[i-1]) != "" {
					nonBlank = true
					break
				}
			}
			if !nonBlank {
				t.Errorf("%s: no non-blank code within ±%d of line_approx %d in %s", e.Name(), window, exp.LineApprox, exp.File)
			}
		})
	}

	if checked == 0 {
		t.Skipf("no bug/perf seeds found under %s", seedsDir)
	}
}
