package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInstallGitHookCmdName(t *testing.T) {
	cmd := &InstallGitHookCmd{}
	if cmd.Name() != "install-git-hook" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "install-git-hook")
	}
}

func TestInstallGitHookCmdImplementsCommand(t *testing.T) {
	var _ Command = &InstallGitHookCmd{}
}

// Finding 2: the prepare-commit-msg hook must verify the marker's diffSHA256
// against the current staged diff — stamping the Reviewed-by-pakka trailer only
// on an exact match, never on ts-freshness alone.
func TestPrepareCommitMsgHook_stampsOnlyOnDiffMatch(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	if _, err := recordReviewPass(repo, ""); err != nil {
		t.Fatal(err)
	}
	hookPath := writeHook(t)

	// Match: staged diff unchanged since the marker → trailer stamped.
	msg := filepath.Join(repo, "MSG_MATCH")
	writeFileT(t, msg, "feat: x\n")
	runHook(t, repo, hookPath, msg)
	if got := readFileT(t, msg); !strings.Contains(got, "Reviewed-by-pakka") {
		t.Fatalf("trailer not stamped on matching diff:\n%s", got)
	}

	// Mismatch: stage another file → hash differs → no trailer.
	stage(t, repo, "b.txt", "world\n")
	msg2 := filepath.Join(repo, "MSG_MISMATCH")
	writeFileT(t, msg2, "feat: y\n")
	runHook(t, repo, hookPath, msg2)
	if got := readFileT(t, msg2); strings.Contains(got, "Reviewed-by-pakka") {
		t.Fatalf("trailer stamped despite diff mismatch:\n%s", got)
	}
}

// A fresh legacy bare-epoch marker carries no diffSHA256 and must not stamp.
func TestPrepareCommitMsgHook_ignoresLegacyMarker(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	dir := filepath.Join(repo, ".pakka", "reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	epoch := strconv.FormatInt(time.Now().Unix(), 10)
	writeFileT(t, filepath.Join(dir, "last-pass-ts"), epoch+"\n")

	hookPath := writeHook(t)
	msg := filepath.Join(repo, "MSG_LEGACY")
	writeFileT(t, msg, "feat: z\n")
	runHook(t, repo, hookPath, msg)
	if got := readFileT(t, msg); strings.Contains(got, "Reviewed-by-pakka") {
		t.Fatalf("trailer stamped from legacy epoch marker:\n%s", got)
	}
}

func writeHook(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prepare-commit-msg")
	if err := os.WriteFile(p, []byte(prepareCommitMsgHook), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func runHook(t *testing.T, repo, hookPath, msgFile string) {
	t.Helper()
	cmd := exec.Command("sh", hookPath, msgFile)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook run: %v\n%s", err, out)
	}
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
