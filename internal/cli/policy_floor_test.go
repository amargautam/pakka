package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/commitgate"
)

// writePolicyFile drops a .pakka/policy.json into repo.
func writePolicyFile(t *testing.T, repo, body string) {
	t.Helper()
	root := repoRootAt(repo)
	if root == "" {
		root = repo
	}
	dir := filepath.Join(root, ".pakka")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeAgedMatchingMarker writes a diff-matching pass marker aged ageSeconds.
func writeAgedMatchingMarker(t *testing.T, repo string, ageSeconds int64) {
	t.Helper()
	root := repoRootAt(repo)
	if root == "" {
		root = repo
	}
	diff, err := stagedDiff(root)
	if err != nil {
		t.Fatalf("stagedDiff: %v", err)
	}
	m := commitgate.PassMarker{
		TS:         time.Now().Unix() - ageSeconds,
		DiffSHA256: sha256Hex(diff),
		Verdict:    "passed",
	}
	b, _ := json.Marshal(m)
	dir := filepath.Join(root, ".pakka", "reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-pass-ts"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFindingsFile writes a non-verdict findings JSONL that loadLatestErrors reads.
func writeFindingsFile(t *testing.T, repo string, lines []string) {
	t.Helper()
	root := repoRootAt(repo)
	if root == "" {
		root = repo
	}
	dir := filepath.Join(root, ".pakka", "reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func auditHasKind(t *testing.T, home, kind string) bool {
	t.Helper()
	dir := filepath.Join(home, ".pakka", "audit")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `"kind":"`+kind+`"`) {
				return true
			}
		}
	}
	return false
}

// auditHasNote reports whether any audit entry contains both the kind and the
// given substring (used to assert policy-state note contents).
func auditHasNote(t *testing.T, home, kind, substr string) bool {
	t.Helper()
	dir := filepath.Join(home, ".pakka", "audit")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `"kind":"`+kind+`"`) && strings.Contains(line, substr) {
				return true
			}
		}
	}
	return false
}

// Finding 4: every gate run writes a policy-state audit note (present/absent).
func TestPolicy_stateNoteWrittenAbsentAndPresent(t *testing.T) {
	// Absent policy.
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	cfg := commitgate.DefaultConfig()
	cfg.SessionID = "sidstate1"
	gatherReviewState(cfg, commitCmd(repo))
	if !auditHasNote(t, home, "policy-state", `absent`) {
		t.Fatal("absent-policy gate run must write a policy-state note")
	}

	// Present policy.
	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	repo2 := initTestRepo(t)
	stage(t, repo2, "a.txt", "hello\n")
	writePolicyFile(t, repo2, `{"v":1,"confidenceThreshold":80}`)
	cfg2 := commitgate.DefaultConfig()
	cfg2.SessionID = "sidstate2"
	gatherReviewState(cfg2, commitCmd(repo2))
	if !auditHasNote(t, home2, "policy-state", `present`) {
		t.Fatal("present-policy gate run must write a policy-state note")
	}
}

// AC1: no policy file → a fresh matching marker authorizes exactly as before.
func TestPolicy_absentFileByteIdenticalPass(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	writeAgedMatchingMarker(t, repo, 10)

	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if !state.HasRecentPass {
		t.Fatalf("no-policy fresh marker must authorize; state=%+v", state)
	}
	if state.PolicyError != "" {
		t.Fatalf("no-policy must not set PolicyError: %q", state.PolicyError)
	}
}

// AC2: policy confidenceThreshold 80 clamps a local 95 down — an 85-confidence
// error now blocks — and an audit "policy-clamp" entry is written.
func TestPolicy_confidenceThresholdClampFiltersAndAudits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	writeFindingsFile(t, repo, []string{
		`{"severity":"error","file":"a.txt","line":1,"confidence":85,"rationale":"boom"}`,
	})
	writePolicyFile(t, repo, `{"v":1,"confidenceThreshold":80}`)

	cfg := commitgate.DefaultConfig()
	cfg.ConfidenceThreshold = 95
	cfg.SessionID = "sidclamp1"

	state, _ := gatherReviewState(cfg, commitCmd(repo))
	if len(state.ErrorFindings) == 0 {
		t.Fatalf("policy floor 80 should surface the 85-confidence finding (local 95 clamped); got none")
	}
	if !auditHasKind(t, home, "policy-clamp") {
		t.Fatal("clamp must write an audit entry of kind policy-clamp")
	}
}

// AC2 negative: a stricter local threshold (below policy) is not clamped.
func TestPolicy_confidenceThresholdStricterLocalNotClamped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	writeFindingsFile(t, repo, []string{
		`{"severity":"error","file":"a.txt","line":1,"confidence":85,"rationale":"boom"}`,
	})
	writePolicyFile(t, repo, `{"v":1,"confidenceThreshold":80}`)

	cfg := commitgate.DefaultConfig()
	cfg.ConfidenceThreshold = 70 // stricter than policy 80
	cfg.SessionID = "sidclamp2"

	gatherReviewState(cfg, commitCmd(repo))
	if auditHasKind(t, home, "policy-clamp") {
		t.Fatal("stricter local threshold must not trigger a clamp")
	}
}

// AC5: a diff-matching marker passes at 25 min (default 1800s window) and fails
// once older than the window; a non-matching marker fails regardless of age.
func TestPolicy_markerFreshnessDefaultWindow(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")

	// 25 min old, matching → pass (1500 < 1800).
	writeAgedMatchingMarker(t, repo, 25*60)
	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if !state.HasRecentPass {
		t.Fatal("25-min matching marker must pass under the 1800s default window")
	}

	// Older than the window → stale, no pass (2000 > 1800).
	writeAgedMatchingMarker(t, repo, 2000)
	state, _ = gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if state.HasRecentPass {
		t.Fatal("marker older than the window must be stale")
	}
}

func TestPolicy_nonMatchingMarkerFailsRegardlessOfAge(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")

	root := repoRootAt(repo)
	m := commitgate.PassMarker{TS: time.Now().Unix(), DiffSHA256: "deadbeef", Verdict: "passed"}
	b, _ := json.Marshal(m)
	dir := filepath.Join(root, ".pakka", "reviews")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "last-pass-ts"), b, 0o644)

	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if state.HasRecentPass {
		t.Fatal("non-matching marker must never authorize")
	}
	if !state.MarkerDiffMismatch {
		t.Fatalf("expected diff mismatch flag; state=%+v", state)
	}
}

// AC6: policy markerFreshnessSeconds 300 clamps the window so a 20-min matching
// marker fails stale.
func TestPolicy_markerFreshnessPolicyLowersWindow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	writeAgedMatchingMarker(t, repo, 20*60) // 1200s
	writePolicyFile(t, repo, `{"v":1,"markerFreshnessSeconds":300}`)

	cfg := commitgate.DefaultConfig()
	cfg.SessionID = "sidfresh"
	state, _ := gatherReviewState(cfg, commitCmd(repo))
	if state.HasRecentPass {
		t.Fatal("policy freshness 300 must fail a 20-min-old marker")
	}
	if !auditHasKind(t, home, "policy-clamp") {
		t.Fatal("freshness clamp must write an audit entry of kind policy-clamp")
	}
}

// AC7: a newer schema version blocks every commit with an upgrade message.
func TestPolicy_newerVersionFailsClosed(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	writeAgedMatchingMarker(t, repo, 10) // would otherwise authorize
	writePolicyFile(t, repo, `{"v":2}`)

	cmd := commitCmd(repo)
	state, _ := gatherReviewState(commitgate.DefaultConfig(), cmd)
	if state.PolicyError == "" {
		t.Fatal("v:2 policy must set state.PolicyError")
	}
	d := commitgate.Evaluate(cmd, commitgate.DefaultConfig(), state)
	if d.Allow {
		t.Fatal("gate must block on a too-new policy even with a fresh matching marker")
	}
	if !strings.Contains(d.Stderr, "version") {
		t.Fatalf("block message must mention version: %q", d.Stderr)
	}
}

// AC7: malformed policy JSON fails closed and names the file.
func TestPolicy_malformedFailsClosed(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	writeAgedMatchingMarker(t, repo, 10)
	writePolicyFile(t, repo, `{"v":1,`)

	cmd := commitCmd(repo)
	state, _ := gatherReviewState(commitgate.DefaultConfig(), cmd)
	d := commitgate.Evaluate(cmd, commitgate.DefaultConfig(), state)
	if d.Allow {
		t.Fatal("gate must block on malformed policy")
	}
	if !strings.Contains(d.Stderr, "policy.json") {
		t.Fatalf("block message must name the policy file: %q", d.Stderr)
	}
}
