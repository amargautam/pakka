package commitgate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- Trailer / pathspec collision (spec AC6) ---

func TestInjectTrailer_splicesBeforePathspecSeparator(t *testing.T) {
	got := InjectTrailer(`git commit -m msg -- file.go`, "Reviewed-by-pakka: v1")
	// The trailer must land before the `--`, not after it.
	sepIdx := strings.Index(got, " -- ")
	trailerIdx := strings.Index(got, "--trailer")
	if trailerIdx < 0 {
		t.Fatalf("no --trailer in %q", got)
	}
	if sepIdx >= 0 && trailerIdx > sepIdx {
		t.Fatalf("--trailer spliced after `--` separator: %q", got)
	}
	if !strings.Contains(got, `--trailer 'Reviewed-by-pakka: v1' -- file.go`) {
		t.Fatalf("unexpected splice: %q", got)
	}
}

func TestInjectTrailer_appendsWhenNoSeparator(t *testing.T) {
	got := InjectTrailer(`git commit -m msg`, "T: v")
	if got != `git commit -m msg --trailer 'T: v'` {
		t.Fatalf("got %q", got)
	}
}

func TestInjectTrailer_ignoresDashDashInsideQuotes(t *testing.T) {
	// A `--` inside a quoted message must not be treated as a pathspec sep.
	got := InjectTrailer(`git commit -m "wip -- notes"`, "T: v")
	if got != `git commit -m "wip -- notes" --trailer 'T: v'` {
		t.Fatalf("got %q", got)
	}
}

// TestEvaluate_TrailerPathspecCommitSucceeds is the end-to-end AC6 regression:
// a passing-gate commit that uses `-- <pathspec>` must, after trailer
// injection, still commit successfully (git must not parse --trailer as a
// pathspec).
func TestEvaluate_TrailerPathspecCommitSucceeds(t *testing.T) {
	repo := initRealRepo(t)
	writeFile(t, filepath.Join(repo, "file.txt"), "hello\n")
	runGit(t, repo, "add", "file.txt")

	cfg := DefaultConfig()
	cfg.Version = "9.9.9"
	state := &State{HasRecentPass: true}

	cmd := "git -C " + shellQuote(repo) + " commit -m msg -- file.txt"
	d := Evaluate(cmd, cfg, state)
	if !d.Allow {
		t.Fatalf("gate blocked a passing commit: %q", d.Stderr)
	}
	if d.Command == "" {
		t.Fatalf("expected a rewritten command with trailers")
	}
	// Sanity: the injected trailer sits before the pathspec separator.
	if i, j := strings.Index(d.Command, "--trailer"), strings.LastIndex(d.Command, " -- "); i > j && j >= 0 {
		t.Fatalf("trailer after separator: %q", d.Command)
	}

	if out, err := exec.Command("sh", "-c", d.Command).CombinedOutput(); err != nil {
		t.Fatalf("rewritten commit failed: %v\n%s\ncmd=%s", err, out, d.Command)
	}
	// Confirm the commit landed and carries the trailer.
	logOut := runGit(t, repo, "log", "-1", "--pretty=%B")
	if !strings.Contains(logOut, "Reviewed-by-pakka") {
		t.Fatalf("trailer not recorded in commit message:\n%s", logOut)
	}
}

// --- No-pass stderr targeting (spec AC3, AC4) ---

func TestEvaluate_DiffMismatchStderr(t *testing.T) {
	cfg := DefaultConfig()
	state := &State{MarkerDiffMismatch: true}
	d := Evaluate(`git commit -m x`, cfg, state)
	if d.Allow {
		t.Fatal("expected block on diff mismatch")
	}
	if !strings.Contains(d.Stderr, "diff mismatch") {
		t.Fatalf("stderr does not name diff mismatch: %q", d.Stderr)
	}
}

func TestEvaluate_LegacyMarkerStderr(t *testing.T) {
	cfg := DefaultConfig()
	state := &State{MarkerLegacy: true}
	d := Evaluate(`git commit -m x`, cfg, state)
	if d.Allow {
		t.Fatal("expected block on legacy marker")
	}
	if !strings.Contains(d.Stderr, "outdated") || !strings.Contains(d.Stderr, "/pakka:review") {
		t.Fatalf("stderr does not instruct re-running review: %q", d.Stderr)
	}
}

// --- helpers ---

func initRealRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "t@t.co")
	runGit(t, dir, "config", "user.name", "t")
	return dir
}

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", repo}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
