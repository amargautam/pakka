package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/commitgate"
)

func TestReviewPassCmdName(t *testing.T) {
	if (&ReviewPassCmd{}).Name() != "review-pass" {
		t.Fatalf("wrong name")
	}
}

func TestReviewPassCmdImplementsCommand(t *testing.T) {
	var _ Command = &ReviewPassCmd{}
}

// Finding 1: review-pass --repo-root writes the marker for a repo other than
// the process CWD, so writer and gate agree when committing a non-CWD repo.
func TestReviewPassRun_repoRootFlagWritesTargetRepo(t *testing.T) {
	target := initTestRepo(t)
	stage(t, target, "a.txt", "hello\n")

	// Exercise Run() (not just recordReviewPass) with the CWD being some other
	// directory. The flag must direct the marker to `target`, not the CWD repo.
	if err := (&ReviewPassCmd{}).Run([]string{"--repo-root", target}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	raw := readMarker(t, target)
	// The marker must bind to target's staged diff (the gate re-hashes the same).
	diff := runGit(t, target, "diff", "--cached")
	if commitgate.ClassifyMarker(raw, sha256Hex([]byte(diff)), time.Now().Unix(), 300) != commitgate.MarkerPass {
		t.Fatalf("marker in target repo did not classify as pass: %q", raw)
	}
	// And gatherReviewState for a commit against target authorizes it.
	state := gatherReviewState(commitgate.DefaultConfig(), commitCmd(target))
	if !state.HasRecentPass {
		t.Fatalf("gate did not see the --repo-root marker; state=%+v", state)
	}
}

// AC1: review-pass on an empty staged diff errors and writes no marker.
func TestRecordReviewPass_emptyDiffErrors(t *testing.T) {
	repo := initTestRepo(t)
	if _, err := recordReviewPass(repo); err == nil {
		t.Fatal("expected error on empty staged diff")
	}
	if _, err := os.Stat(filepath.Join(repo, ".pakka", "reviews", "last-pass-ts")); err == nil {
		t.Fatal("marker written despite empty diff")
	}
}

// AC1: review-pass with a staged diff writes a JSON marker with verdict passed.
func TestRecordReviewPass_writesJSONMarker(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")

	marker, err := recordReviewPass(repo)
	if err != nil {
		t.Fatalf("recordReviewPass: %v", err)
	}
	if marker.Verdict != "passed" || marker.DiffSHA256 == "" || marker.TS == 0 {
		t.Fatalf("bad marker: %+v", marker)
	}
	// On disk it must be JSON (not a bare epoch).
	raw := readMarker(t, repo)
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		t.Fatalf("marker not JSON: %q", raw)
	}
	if commitgate.ClassifyMarker(raw, marker.DiffSHA256, time.Now().Unix(), 300) != commitgate.MarkerPass {
		t.Fatalf("fresh marker did not classify as pass")
	}
}

// AC2: gate allows commit when marker is fresh and diff matches.
func TestGate_freshMatchingMarkerPasses(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	if _, err := recordReviewPass(repo); err != nil {
		t.Fatal(err)
	}

	state := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if !state.HasRecentPass {
		t.Fatalf("expected HasRecentPass; state=%+v", state)
	}
	d := commitgate.Evaluate(commitCmd(repo), commitgate.DefaultConfig(), state)
	if !d.Allow {
		t.Fatalf("gate blocked a matching pass: %q", d.Stderr)
	}
}

// AC3: staging an extra file after the marker blocks with a diff-mismatch message.
func TestGate_diffChangedAfterMarkerBlocks(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	if _, err := recordReviewPass(repo); err != nil {
		t.Fatal(err)
	}
	// Stage an additional file — diff no longer matches the marker.
	stage(t, repo, "b.txt", "world\n")

	state := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if state.HasRecentPass {
		t.Fatal("stale-diff marker wrongly authorized")
	}
	if !state.MarkerDiffMismatch {
		t.Fatalf("expected MarkerDiffMismatch; state=%+v", state)
	}
	d := commitgate.Evaluate(commitCmd(repo), commitgate.DefaultConfig(), state)
	if d.Allow || !strings.Contains(d.Stderr, "diff mismatch") {
		t.Fatalf("expected mismatch block; allow=%v stderr=%q", d.Allow, d.Stderr)
	}
}

// AC5: identical re-staging yields the same hash — commit still passes.
func TestGate_identicalRestagePasses(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	if _, err := recordReviewPass(repo); err != nil {
		t.Fatal(err)
	}
	// Unstage and restage the identical content.
	runGit(t, repo, "reset", "-q")
	stage(t, repo, "a.txt", "hello\n")

	state := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if !state.HasRecentPass {
		t.Fatalf("identical restage did not pass; state=%+v", state)
	}
}

// AC4: a legacy bare-epoch marker is rejected with an upgrade message.
func TestGate_legacyEpochMarkerBlocks(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	// Hand-write a fresh legacy marker (pre-JSON format).
	dir := filepath.Join(repo, ".pakka", "reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	epoch := strconv.FormatInt(time.Now().Unix(), 10)
	if err := os.WriteFile(filepath.Join(dir, "last-pass-ts"), []byte(epoch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if state.HasRecentPass {
		t.Fatal("legacy marker wrongly authorized")
	}
	if !state.MarkerLegacy {
		t.Fatalf("expected MarkerLegacy; state=%+v", state)
	}
	d := commitgate.Evaluate(commitCmd(repo), commitgate.DefaultConfig(), state)
	if d.Allow || !strings.Contains(d.Stderr, "outdated") {
		t.Fatalf("expected legacy block; allow=%v stderr=%q", d.Allow, d.Stderr)
	}
}

// --- helpers ---

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

func readMarker(t *testing.T, repo string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, ".pakka", "reviews", "last-pass-ts"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return string(b)
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
