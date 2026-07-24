package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/hookevent"
	"github.com/amargautam/pakka/internal/meter"
)

func TestMeterCmdName(t *testing.T) {
	cmd := &MeterCmd{}
	if cmd.Name() != "meter" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "meter")
	}
}

func TestMeterCmdRunWritesMeterFile(t *testing.T) {
	// MeterCmd.Run should not panic on empty/missing event
	// and should complete without error when HOME is a temp dir
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cmd := &MeterCmd{}
	// Empty args — should not panic, may return error but not panic
	_ = cmd.Run(nil)
}

// readSessionEndRepo runs runMeterSessionEnd for (sid, cwd) and returns the
// repo tag of the written session-end entry.
func readSessionEndRepo(t *testing.T, home, sid, cwd string) string {
	t.Helper()
	runMeterSessionEnd(&hookevent.Event{SessionID: sid, CWD: cwd})

	data, err := os.ReadFile(filepath.Join(home, ".pakka", "meter", sid+".jsonl"))
	if err != nil {
		t.Fatalf("session-end entry not written for cwd %q: %v", cwd, err)
	}
	var entry meter.Entry
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatal(err)
	}
	return entry.Repo
}

// TestRunMeterSessionEndRepoRootVariesWithCWD proves session-end snapshots
// carry a canonical repo_root tag that tracks the session cwd (#10):
//   - a cwd inside a git repo tags the symlink-resolved git toplevel;
//   - a cwd in a plain dir (multi-repo workspace shape) tags the
//     canonicalized dir itself;
//   - two different cwds produce two different tags.
func TestRunMeterSessionEndRepoRootVariesWithCWD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Workspace-shaped cwd: plain dir, no git repo.
	workspace := filepath.Join(t.TempDir(), "multi-repo-workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	gotWorkspace := readSessionEndRepo(t, home, "sess-end-workspace", workspace)
	if gotWorkspace != wantWorkspace {
		t.Errorf("workspace cwd: repo = %q, want canonical cwd %q", gotWorkspace, wantWorkspace)
	}

	// Git-repo cwd: a subdirectory must tag the repo toplevel.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := filepath.Join(t.TempDir(), "real-repo")
	sub := filepath.Join(repo, "cmd", "deep")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Skipf("git init failed: %v: %s", err, out)
	}
	wantRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	gotRepo := readSessionEndRepo(t, home, "sess-end-gitrepo", sub)
	if gotRepo != wantRepo {
		t.Errorf("git subdir cwd: repo = %q, want toplevel %q", gotRepo, wantRepo)
	}

	if gotWorkspace == gotRepo {
		t.Errorf("repo_root must vary with cwd; both sessions tagged %q", gotRepo)
	}
}

// TestRunMeterSessionEndEmptyCWDFallsBackNonEmpty proves the consistency
// guarantee: even when the hook event carries no cwd, the snapshot still
// gets a non-empty canonical tag (derived from the process cwd).
func TestRunMeterSessionEndEmptyCWDFallsBackNonEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := readSessionEndRepo(t, home, "sess-end-nocwd", "")
	if got == "" {
		t.Error("repo = \"\"; session-end snapshot must always carry a non-empty repo_root")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := meter.RepoKey(wd)
	if got != want {
		t.Errorf("repo = %q, want process-cwd fallback %q", got, want)
	}
}
