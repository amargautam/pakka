package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackfillOutputTokens drives backfillOutputTokens against a synthetic
// meter dir and synthetic transcripts dir. After the call, every meter file
// should contain at least one line with output_tokens matching the sum of
// output_tokens across that session's transcripts.
func TestBackfillOutputTokens(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	projectsDir := filepath.Join(tmp, "projects")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Session A: meter file with session_id "aaa-1111-2222-3333-4444";
	// transcript with two assistant lines summing to 700 output tokens.
	sessA := "aaa-1111-2222-3333-4444"
	meterA := `{"ts":"2026-05-01T10:00:00Z","session_id":"` + sessA + `","repo":"/r","tokens_used":100}` + "\n"
	if err := os.WriteFile(filepath.Join(meterDir, "aaa-1111.jsonl"), []byte(meterA), 0644); err != nil {
		t.Fatal(err)
	}
	projA := filepath.Join(projectsDir, "proj-a")
	if err := os.MkdirAll(projA, 0755); err != nil {
		t.Fatal(err)
	}
	transA := `{"message":{"usage":{"output_tokens":300}}}` + "\n" +
		`{"message":{"usage":{"output_tokens":400}}}` + "\n"
	if err := os.WriteFile(filepath.Join(projA, sessA+".jsonl"), []byte(transA), 0644); err != nil {
		t.Fatal(err)
	}

	// Session B: meter has session_id "bbb-9999-8888-7777-6666";
	// transcript with one line of 1500 output tokens.
	sessB := "bbb-9999-8888-7777-6666"
	meterB := `{"ts":"2026-05-02T10:00:00Z","session_id":"` + sessB + `","repo":"/r","bytes_saved":50}` + "\n"
	if err := os.WriteFile(filepath.Join(meterDir, "bbb-9999.jsonl"), []byte(meterB), 0644); err != nil {
		t.Fatal(err)
	}
	projB := filepath.Join(projectsDir, "proj-b")
	if err := os.MkdirAll(projB, 0755); err != nil {
		t.Fatal(err)
	}
	transB := `{"message":{"usage":{"output_tokens":1500}}}` + "\n"
	if err := os.WriteFile(filepath.Join(projB, sessB+".jsonl"), []byte(transB), 0644); err != nil {
		t.Fatal(err)
	}

	// Session C: meter exists, no matching transcript → output_tokens=0
	// should still be written (idempotent baseline).
	sessC := "ccc-no-transcript-here-zzzz"
	meterC := `{"ts":"2026-05-03T10:00:00Z","session_id":"` + sessC + `","repo":"/r","tokens_used":10}` + "\n"
	if err := os.WriteFile(filepath.Join(meterDir, "ccc-nooo.jsonl"), []byte(meterC), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := backfillOutputTokens(meterDir, projectsDir, false)
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]int64{
		filepath.Join(meterDir, "aaa-1111.jsonl"): 700,
		filepath.Join(meterDir, "bbb-9999.jsonl"): 1500,
		filepath.Join(meterDir, "ccc-nooo.jsonl"): 0,
	}
	for path, want := range checks {
		got := maxOutputTokensInFile(t, path)
		if got != want {
			t.Errorf("%s: max output_tokens = %d, want %d", filepath.Base(path), got, want)
		}
	}

	if stats.FilesProcessed != 3 {
		t.Errorf("FilesProcessed = %d, want 3", stats.FilesProcessed)
	}
	if stats.TotalOutputTokens != 2200 {
		t.Errorf("TotalOutputTokens = %d, want 2200", stats.TotalOutputTokens)
	}

	// Idempotency: second invocation must not change totals or duplicate writes.
	beforeBytes, _ := os.ReadFile(filepath.Join(meterDir, "aaa-1111.jsonl"))
	stats2, err := backfillOutputTokens(meterDir, projectsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, _ := os.ReadFile(filepath.Join(meterDir, "aaa-1111.jsonl"))
	if string(beforeBytes) != string(afterBytes) {
		t.Errorf("second run mutated aaa-1111.jsonl; want idempotent\nbefore=%q\nafter=%q", beforeBytes, afterBytes)
	}
	if stats2.FilesProcessed != 0 {
		t.Errorf("second run FilesProcessed = %d, want 0 (all skipped)", stats2.FilesProcessed)
	}
}

func TestBackfillOutputTokensDryRun(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	projectsDir := filepath.Join(tmp, "projects")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}

	sess := "dryrun01-2222-3333-4444-5555"
	meterContent := `{"ts":"2026-05-01T10:00:00Z","session_id":"` + sess + `","repo":"/r","tokens_used":100}` + "\n"
	meterPath := filepath.Join(meterDir, "dryrun01.jsonl")
	if err := os.WriteFile(meterPath, []byte(meterContent), 0644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(projectsDir, "proj")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}
	trans := `{"message":{"usage":{"output_tokens":999}}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, sess+".jsonl"), []byte(trans), 0644); err != nil {
		t.Fatal(err)
	}

	before, _ := os.ReadFile(meterPath)
	if _, err := backfillOutputTokens(meterDir, projectsDir, true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(meterPath)
	if string(before) != string(after) {
		t.Errorf("dry-run mutated file; before=%q after=%q", before, after)
	}
}

// TestBackfillRederivesRepoTags drives the repo_root re-derivation pass (#10)
// against a mixed tagged/untagged fixture:
//   - untagged entries whose sessions have transcripts recording DIFFERENT
//     cwds must be tagged with different canonical repo roots (tag varies
//     with the transcript cwd, canonicalized like session-end tagging);
//   - already-tagged entries must be preserved byte-relevant (tag unchanged);
//   - untagged entries with no surviving transcript stay untagged;
//   - a second run is a no-op (idempotent).
func TestBackfillRederivesRepoTags(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	projectsDir := filepath.Join(tmp, "projects")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Two real (non-git) cwds so canonicalization is exercised for the
	// multi-repo workspace shape.
	cwdAlpha := filepath.Join(tmp, "ws", "alpha")
	cwdBeta := filepath.Join(tmp, "ws", "beta")
	for _, d := range []string{cwdAlpha, cwdBeta} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	wantAlpha, err := filepath.EvalSymlinks(cwdAlpha)
	if err != nil {
		t.Fatal(err)
	}
	wantBeta, err := filepath.EvalSymlinks(cwdBeta)
	if err != nil {
		t.Fatal(err)
	}

	// Untagged v0.9.0-style backfill entries (output_tokens > 0, no repo).
	// Each session has a transcript in its own project dir recording its cwd.
	sessAlpha := "alpha-sess-1111-2222-3333"
	sessBeta := "beta-sess-4444-5555-6666"
	sessNoTrans := "ghost-sess-7777-8888-9999"
	writeFixture := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(meterDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture("alpha.jsonl",
		`{"ts":"2026-05-01T10:00:00Z","session_id":"`+sessAlpha+`","output_tokens":700,"source":"backfill"}`+"\n")
	writeFixture("beta.jsonl",
		`{"ts":"2026-05-02T10:00:00Z","session_id":"`+sessBeta+`","output_tokens":900,"source":"backfill"}`+"\n")
	writeFixture("ghost.jsonl",
		`{"ts":"2026-05-03T10:00:00Z","session_id":"`+sessNoTrans+`","output_tokens":50,"source":"backfill"}`+"\n")
	// Already-tagged entry: must come out with its tag intact.
	taggedLine := `{"ts":"2026-05-04T10:00:00Z","session_id":"tagged-sess","repo":"/already/tagged","output_tokens":1234}`
	writeFixture("tagged.jsonl", taggedLine+"\n")

	for sid, cwd := range map[string]string{sessAlpha: cwdAlpha, sessBeta: cwdBeta} {
		proj := filepath.Join(projectsDir, "proj-"+sid[:5])
		if err := os.MkdirAll(proj, 0755); err != nil {
			t.Fatal(err)
		}
		trans := `{"cwd":"` + cwd + `","message":{"usage":{"output_tokens":10}}}` + "\n"
		if err := os.WriteFile(filepath.Join(proj, sid+".jsonl"), []byte(trans), 0644); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := backfillOutputTokens(meterDir, projectsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.EntriesRetagged != 2 {
		t.Errorf("EntriesRetagged = %d, want 2 (alpha + beta; ghost has no transcript, tagged.jsonl already tagged)", stats.EntriesRetagged)
	}

	if got := repoTagForSession(t, filepath.Join(meterDir, "alpha.jsonl"), sessAlpha); got != wantAlpha {
		t.Errorf("alpha entry repo = %q, want canonical transcript cwd %q", got, wantAlpha)
	}
	if got := repoTagForSession(t, filepath.Join(meterDir, "beta.jsonl"), sessBeta); got != wantBeta {
		t.Errorf("beta entry repo = %q, want canonical transcript cwd %q", got, wantBeta)
	}
	if wantAlpha == wantBeta {
		t.Fatal("fixture broken: alpha and beta cwds must canonicalize differently")
	}
	if got := repoTagForSession(t, filepath.Join(meterDir, "ghost.jsonl"), sessNoTrans); got != "" {
		t.Errorf("ghost entry repo = %q, want untagged (no transcript to derive from)", got)
	}
	if got := repoTagForSession(t, filepath.Join(meterDir, "tagged.jsonl"), "tagged-sess"); got != "/already/tagged" {
		t.Errorf("tagged entry repo = %q, want preserved /already/tagged", got)
	}

	// Idempotency: a second run must not modify any file.
	before := map[string]string{}
	for _, name := range []string{"alpha.jsonl", "beta.jsonl", "ghost.jsonl", "tagged.jsonl"} {
		bs, err := os.ReadFile(filepath.Join(meterDir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = string(bs)
	}
	stats2, err := backfillOutputTokens(meterDir, projectsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.EntriesRetagged != 0 {
		t.Errorf("second run EntriesRetagged = %d, want 0", stats2.EntriesRetagged)
	}
	for name, want := range before {
		bs, err := os.ReadFile(filepath.Join(meterDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(bs) != want {
			t.Errorf("second run mutated %s; want idempotent\nbefore=%q\nafter=%q", name, want, bs)
		}
	}
}

// TestBackfillNewEntriesCarryRepoTag proves freshly appended backfill entries
// (token pass + orphan pass) carry a repo tag derived from the transcript cwd.
func TestBackfillNewEntriesCarryRepoTag(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	projectsDir := filepath.Join(tmp, "projects")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}

	cwd := filepath.Join(tmp, "ws", "gamma")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}

	// Meter file with no output_tokens yet → token pass appends an entry.
	sess := "gamma-sess-1111-2222-3333"
	meterLine := `{"ts":"2026-05-01T10:00:00Z","session_id":"` + sess + `","tokens_used":10}` + "\n"
	if err := os.WriteFile(filepath.Join(meterDir, "gamma.jsonl"), []byte(meterLine), 0644); err != nil {
		t.Fatal(err)
	}
	// Orphan session: transcript only, no meter file.
	orphanSess := "orphan-sess-4444-5555-6666"

	proj := filepath.Join(projectsDir, "proj-g")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}
	trans := `{"cwd":"` + cwd + `","message":{"usage":{"output_tokens":42}}}` + "\n"
	for _, sid := range []string{sess, orphanSess} {
		if err := os.WriteFile(filepath.Join(proj, sid+".jsonl"), []byte(trans), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := backfillOutputTokens(meterDir, projectsDir, false); err != nil {
		t.Fatal(err)
	}

	if got := repoTagForSession(t, filepath.Join(meterDir, "gamma.jsonl"), sess); got != want {
		t.Errorf("backfill entry repo = %q, want canonical transcript cwd %q", got, want)
	}
	orphanPath := filepath.Join(meterDir, "orphan-"+shortHash(orphanSess)+".jsonl")
	if got := repoTagForSession(t, orphanPath, orphanSess); got != want {
		t.Errorf("orphan entry repo = %q, want canonical transcript cwd %q", got, want)
	}
}

// repoTagForSession returns the repo tag of the last line in path matching
// session_id == sid ("" when untagged or no such line).
func repoTagForSession(t *testing.T, path, sid string) string {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	repo := ""
	for _, line := range strings.Split(strings.TrimSpace(string(bs)), "\n") {
		if line == "" {
			continue
		}
		var probe struct {
			SessionID string `json:"session_id"`
			Repo      string `json:"repo"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		if probe.SessionID == sid {
			repo = probe.Repo
		}
	}
	return repo
}

// maxOutputTokensInFile returns the largest output_tokens value across all
// JSONL lines in path.
func maxOutputTokensInFile(t *testing.T, path string) int64 {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var max int64
	for _, line := range strings.Split(strings.TrimSpace(string(bs)), "\n") {
		if line == "" {
			continue
		}
		var probe struct {
			OutputTokens int64 `json:"output_tokens"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		if probe.OutputTokens > max {
			max = probe.OutputTokens
		}
	}
	return max
}
