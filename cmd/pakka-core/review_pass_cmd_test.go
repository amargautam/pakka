package main

import (
	"encoding/json"
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
	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(target))
	if !state.HasRecentPass {
		t.Fatalf("gate did not see the --repo-root marker; state=%+v", state)
	}
}

// AC1: review-pass on an empty staged diff errors and writes no marker.
func TestRecordReviewPass_emptyDiffErrors(t *testing.T) {
	repo := initTestRepo(t)
	if _, err := recordReviewPass(repo, ""); err == nil {
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

	marker, err := recordReviewPass(repo, "")
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
	if _, err := recordReviewPass(repo, ""); err != nil {
		t.Fatal(err)
	}

	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
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
	if _, err := recordReviewPass(repo, ""); err != nil {
		t.Fatal(err)
	}
	// Stage an additional file — diff no longer matches the marker.
	stage(t, repo, "b.txt", "world\n")

	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
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
	if _, err := recordReviewPass(repo, ""); err != nil {
		t.Fatal(err)
	}
	// Unstage and restage the identical content.
	runGit(t, repo, "reset", "-q")
	stage(t, repo, "a.txt", "hello\n")

	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
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

	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
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

// AC1: review-pass --findings records findingsSHA256 + a severity tally on the
// marker matching the file's contents.
func TestRecordReviewPass_bindsFindings(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")

	findings := writeFindings(t, repo, []string{
		`{"kind":"security","severity":"error","file":"a.txt","line":1,"confidence":90,"rationale":"unsanitized shell interpolation"}`,
		`{"kind":"correctness","severity":"warn","file":"a.txt","line":2,"confidence":70,"rationale":"nil deref risk"}`,
		`{"kind":"style","severity":"info","file":"a.txt","line":3,"confidence":50,"rationale":"rename variable"}`,
		`{"kind":"misc","severity":"nit","file":"a.txt","line":4,"confidence":40,"rationale":"unknown severity counts toward total only"}`,
	})

	marker, err := recordReviewPass(repo, findings)
	if err != nil {
		t.Fatalf("recordReviewPass: %v", err)
	}
	raw, _ := os.ReadFile(findings)
	if marker.FindingsSHA256 != sha256Hex(raw) {
		t.Fatalf("findingsSHA256 mismatch: got %q want %q", marker.FindingsSHA256, sha256Hex(raw))
	}
	if marker.FindingsCounts == nil {
		t.Fatal("FindingsCounts nil")
	}
	got := *marker.FindingsCounts
	if got.Error != 1 || got.Warning != 1 || got.Info != 1 || got.Total != 4 {
		t.Fatalf("bad counts: %+v", got)
	}
	if marker.FindingsPath == "" {
		t.Fatal("FindingsPath empty; gate cannot re-resolve the findings file")
	}
	// Path stored repo-relative so the gate resolves it from the repo root.
	if filepath.IsAbs(marker.FindingsPath) {
		t.Fatalf("FindingsPath should be repo-relative, got absolute: %q", marker.FindingsPath)
	}
}

// AC1: tallyFindings buckets the reviewer agents' actual vocabulary. Agents
// emit "warn" (agents/*.md); "warning" is accepted as an alias. Unknown/absent
// severities count toward Total only; unparseable lines are ignored.
func TestTallyFindings_severityVocabulary(t *testing.T) {
	blob := strings.Join([]string{
		`{"severity":"error"}`,
		`{"severity":"warn"}`,    // agents' real vocabulary
		`{"severity":"warning"}`, // legacy/hand-written alias
		`{"severity":"info"}`,
		`{"severity":"nit"}`, // unknown → total only
		`{"kind":"x"}`,       // absent severity → total only
		`not json`,           // ignored entirely
		``,                   // blank → skipped
	}, "\n")

	got := tallyFindings([]byte(blob))
	want := commitgate.FindingsCounts{Error: 1, Warning: 2, Info: 1, Total: 6}
	if got != want {
		t.Fatalf("tallyFindings = %+v; want %+v", got, want)
	}
}

// AC1: an unreadable --findings path errors and writes no marker.
func TestRecordReviewPass_unreadableFindingsErrors(t *testing.T) {
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")

	_, err := recordReviewPass(repo, filepath.Join(repo, "does-not-exist.jsonl"))
	if err == nil {
		t.Fatal("expected error for unreadable findings path")
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".pakka", "reviews", "last-pass-ts")); statErr == nil {
		t.Fatal("marker written despite unreadable findings")
	}
}

// AC2: a marker recorded WITHOUT --findings is byte-shape identical to the
// v0.15 marker — no findings fields leak in via non-omitempty tags.
func TestPassMarker_v015ByteShapeUnchanged(t *testing.T) {
	// Frozen v0.15 fixture: exactly {ts, diffSHA256, verdict}.
	const v015 = `{"ts":1780439832,"diffSHA256":"abc123","verdict":"passed"}`
	m := commitgate.PassMarker{TS: 1780439832, DiffSHA256: "abc123", Verdict: "passed"}
	got, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != v015 {
		t.Fatalf("marker byte shape drifted from v0.15\n got: %s\nwant: %s", got, v015)
	}

	// And the on-disk marker from recordReviewPass(root, "") carries no findings keys.
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	if _, err := recordReviewPass(repo, ""); err != nil {
		t.Fatal(err)
	}
	raw := readMarker(t, repo)
	if strings.Contains(raw, "findings") {
		t.Fatalf("marker without --findings leaked findings keys: %q", raw)
	}
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
