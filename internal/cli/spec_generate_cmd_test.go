package cli

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSpecGenerateCmdName(t *testing.T) {
	if (&SpecGenerateCmd{}).Name() != "spec-generate" {
		t.Fatalf("wrong name")
	}
}

func TestSpecGenerateCmdImplementsCommand(t *testing.T) {
	var _ Command = &SpecGenerateCmd{}
}

// gitInit initializes a git repo in dir (needed so rev-parse --show-toplevel
// resolves) and returns dir.
func gitInit(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

// AC4: outside any git repo -> error, no directory resolved. Uses the CWD
// branch (no --repo-root) with the CWD set to a non-repo temp dir.
func TestResolveSpecsDir_outsideRepoErrors(t *testing.T) {
	t.Chdir(t.TempDir()) // a bare temp dir is not a git repo
	if _, err := resolveSpecsDir("", "docs/specs/"); err == nil {
		t.Fatal("expected error outside any git repo, got nil")
	}
}

// AC5: from a repo SUBDIRECTORY, the spec dir anchors to <git-toplevel>/docs/specs,
// not the CWD. Exercised via the CWD branch with CWD set to a nested subdir.
func TestResolveSpecsDir_subdirAnchorsToToplevel(t *testing.T) {
	top := gitInit(t, t.TempDir())
	sub := filepath.Join(top, "a", "b")
	if err := exec.Command("mkdir", "-p", sub).Run(); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	t.Chdir(sub)

	got, err := resolveSpecsDir("", "docs/specs/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// git may resolve the toplevel through /private on macOS; compare via
	// EvalSymlinks so the toplevel prefix matches regardless.
	want := filepath.Join(evalOrSame(t, top), "docs/specs")
	if evalOrSame(t, got) != want {
		t.Errorf("got %q, want %q (must anchor to toplevel, not CWD)", got, want)
	}
}

// AC6: --repo-root pointing at a valid git repo writes under <path>/docs/specs.
func TestResolveSpecsDir_repoRootValid(t *testing.T) {
	top := gitInit(t, t.TempDir())
	got, err := resolveSpecsDir(top, "docs/specs/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(evalOrSame(t, top), "docs/specs")
	if evalOrSame(t, got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// AC6: --repo-root pointing at a subdir of a repo still anchors to the toplevel.
func TestResolveSpecsDir_repoRootSubdirAnchorsToplevel(t *testing.T) {
	top := gitInit(t, t.TempDir())
	sub := filepath.Join(top, "nested")
	if err := exec.Command("mkdir", "-p", sub).Run(); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	got, err := resolveSpecsDir(sub, "docs/specs/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(evalOrSame(t, top), "docs/specs")
	if evalOrSame(t, got) != want {
		t.Errorf("got %q, want %q (must anchor to toplevel, not the passed subdir)", got, want)
	}
}

// AC6: --repo-root that does not exist -> error.
func TestResolveSpecsDir_repoRootNonexistentErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := resolveSpecsDir(missing, "docs/specs/"); err == nil {
		t.Fatal("expected error for nonexistent --repo-root, got nil")
	}
}

// AC6: --repo-root that exists but is not a git repo -> error.
func TestResolveSpecsDir_repoRootNonRepoErrors(t *testing.T) {
	dir := t.TempDir() // exists, but not a git repo
	if _, err := resolveSpecsDir(dir, "docs/specs/"); err == nil {
		t.Fatal("expected error for non-repo --repo-root, got nil")
	}
}

// An absolute --specs-dir is honored verbatim (not re-anchored to the toplevel).
func TestResolveSpecsDir_absoluteSpecsDirVerbatim(t *testing.T) {
	top := gitInit(t, t.TempDir())
	abs := filepath.Join(t.TempDir(), "elsewhere", "specs")
	got, err := resolveSpecsDir(top, abs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != abs {
		t.Errorf("absolute specs-dir must be verbatim: got %q, want %q", got, abs)
	}
}

// evalOrSame returns filepath.EvalSymlinks(p) or p if that fails, so toplevel
// comparisons survive macOS's /var -> /private/var symlinking.
func evalOrSame(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
