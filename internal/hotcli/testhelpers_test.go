package hotcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Test helpers shared across the hotcli test files. These mirror the copies in
// internal/cli's test package (the commit-gate tests moved here with the
// command, and can't reach cli's test-only helpers across the package boundary).

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "t@t.co")
	runGit(t, dir, "config", "user.name", "t")
	return dir
}

func stage(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", name)
}

func commitCmd(repo string) string {
	return "git -C " + repo + " commit -m x"
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

// writeFindings writes JSONL lines to <repo>/.pakka/reviews/verdict-test.jsonl
// and returns the absolute path.
func writeFindings(t *testing.T, repo string, lines []string) string {
	t.Helper()
	dir := filepath.Join(repo, ".pakka", "reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "verdict-test.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
