package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/commitgate"
	"github.com/amargautam/pakka/internal/recall"
)

// stageFindingsPass stages a file, writes a findings JSONL, records a
// findings-bound review pass, and returns (repo, findingsPath).
func stageFindingsPass(t *testing.T, lines []string) (string, string) {
	t.Helper()
	repo := initTestRepo(t)
	stage(t, repo, "a.txt", "hello\n")
	findings := writeFindings(t, repo, lines)
	if _, err := recordReviewPass(repo, findings); err != nil {
		t.Fatalf("recordReviewPass with findings: %v", err)
	}
	return repo, findings
}

// AC3: an authorized findings-bound pass writes a "review-verdict" audit entry
// whose rationale text is retrievable via recall's FTS5 index.
func TestReviewVerdict_auditWrittenAndRecallable(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	repo, _ := stageFindingsPass(t, []string{
		`{"kind":"security","severity":"error","file":"a.txt","line":1,"confidence":90,"rationale":"quixotic goldfish injection vector"}`,
		`{"kind":"correctness","severity":"warn","file":"a.txt","line":2,"confidence":70,"rationale":"boundary off by one"}`,
	})

	// Gate gathers state + returns the ReviewVerdict courier populated with the
	// findings rationale.
	state, verdict := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if !state.HasRecentPass {
		t.Fatalf("findings-bound pass not authorized; state=%+v", state)
	}
	if verdict == nil {
		t.Fatal("ReviewVerdict not returned for findings-bound pass")
	}
	if !strings.Contains(verdict.Rationales, "quixotic goldfish") {
		t.Fatalf("rationales missing finding prose: %q", verdict.Rationales)
	}

	// Drive the exact production audit-write path.
	sid := "sessABC123"
	d := &commitgate.Decision{Allow: true, IsCommit: true}
	maybeWriteReviewVerdict(sid, d, verdict)

	// Index the audit dir and query for a rationale keyword.
	auditDir := filepath.Join(tmpHome, ".pakka", "audit")
	dbPath := filepath.Join(tmpHome, "recall.db")
	if err := recall.Index(dbPath, auditDir); err != nil {
		t.Fatalf("recall.Index: %v", err)
	}
	entries, err := recall.Query(dbPath, "quixotic", 10)
	if err != nil {
		t.Fatalf("recall.Query: %v", err)
	}
	// The FTS5 MATCH on "quixotic" hits the full indexed content (the whole
	// JSON line), so a returned review-verdict row proves the rationale text is
	// searchable — even though the 120-rune Preview truncates before it.
	var found bool
	for _, e := range entries {
		if e.Kind == "review-verdict" {
			found = true
		}
	}
	if !found {
		t.Fatalf("review-verdict entry not returned by FTS5 query on a rationale keyword; entries=%+v", entries)
	}
}

// AC3 negative: without a bound findings decision, no verdict entry is written.
func TestReviewVerdict_noWriteWhenBlocked(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	repo, _ := stageFindingsPass(t, []string{
		`{"kind":"security","severity":"error","file":"a.txt","line":1,"confidence":90,"rationale":"zzz"}`,
	})
	_, verdict := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	// Blocked decision must not write, even with a bound verdict present.
	maybeWriteReviewVerdict("sid", &commitgate.Decision{Allow: false, IsCommit: true}, verdict)
	dbPath := filepath.Join(tmpHome, "recall.db")
	_ = recall.Index(dbPath, filepath.Join(tmpHome, ".pakka", "audit"))
	entries, _ := recall.Query(dbPath, "zzz", 10)
	for _, e := range entries {
		if e.Kind == "review-verdict" {
			t.Fatalf("verdict written for a blocked decision: %+v", e)
		}
	}
}

// AC4: a gate-injected commit's trailer carries diff:<8hex> and, with bound
// findings, findings:<8hex> plus error/warning counts — asserted via git log.
func TestGate_trailerCarriesProvenance(t *testing.T) {
	repo, _ := stageFindingsPass(t, []string{
		`{"kind":"security","severity":"error","file":"a.txt","line":1,"confidence":90,"rationale":"r1"}`,
		`{"kind":"security","severity":"error","file":"a.txt","line":2,"confidence":90,"rationale":"r2"}`,
		`{"kind":"correctness","severity":"warn","file":"a.txt","line":3,"confidence":70,"rationale":"r3"}`,
	})

	cmd := commitCmd(repo)
	state, _ := gatherReviewState(commitgate.DefaultConfig(), cmd)
	d := commitgate.Evaluate(cmd, commitgate.DefaultConfig(), state)
	if !d.Allow {
		t.Fatalf("gate blocked authorized commit: %q", d.Stderr)
	}
	if d.Command == "" {
		t.Fatal("no rewritten command (trailers not injected)")
	}

	// Execute the gate-rewritten commit.
	out, err := exec.Command("bash", "-c", d.Command).CombinedOutput()
	if err != nil {
		t.Fatalf("commit failed: %v\n%s", err, out)
	}

	body := runGit(t, repo, "log", "-1", "--format=%B")
	if !strings.Contains(body, "diff:") {
		t.Fatalf("trailer missing diff digest:\n%s", body)
	}
	if !strings.Contains(body, "findings:") {
		t.Fatalf("trailer missing findings digest:\n%s", body)
	}
	if !strings.Contains(body, "2 errors") || !strings.Contains(body, "1 warnings") {
		t.Fatalf("trailer missing error/warning counts:\n%s", body)
	}
}

// AC5: mutating the findings file after review-pass blocks the commit with a
// findings-mismatch message.
func TestGate_findingsMutatedAfterPassBlocks(t *testing.T) {
	repo, findings := stageFindingsPass(t, []string{
		`{"kind":"security","severity":"error","file":"a.txt","line":1,"confidence":90,"rationale":"r1"}`,
	})

	// Swap the evidence after the pass was recorded.
	if err := os.WriteFile(findings, []byte(`{"kind":"security","severity":"info","file":"a.txt","line":1,"confidence":10,"rationale":"downgraded"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := commitCmd(repo)
	state, _ := gatherReviewState(commitgate.DefaultConfig(), cmd)
	if state.HasRecentPass {
		t.Fatal("swapped-evidence pass wrongly authorized")
	}
	if !state.MarkerFindingsMismatch {
		t.Fatalf("expected MarkerFindingsMismatch; state=%+v", state)
	}
	d := commitgate.Evaluate(cmd, commitgate.DefaultConfig(), state)
	if d.Allow || !strings.Contains(d.Stderr, "findings mismatch") {
		t.Fatalf("expected findings-mismatch block; allow=%v stderr=%q", d.Allow, d.Stderr)
	}
}

// AC5 variant: a missing findings file (deleted after pass) also blocks.
func TestGate_findingsDeletedAfterPassBlocks(t *testing.T) {
	repo, findings := stageFindingsPass(t, []string{
		`{"kind":"security","severity":"error","file":"a.txt","line":1,"confidence":90,"rationale":"r1"}`,
	})
	if err := os.Remove(findings); err != nil {
		t.Fatal(err)
	}
	state, _ := gatherReviewState(commitgate.DefaultConfig(), commitCmd(repo))
	if state.HasRecentPass || !state.MarkerFindingsMismatch {
		t.Fatalf("deleted findings not treated as mismatch; state=%+v", state)
	}
}
