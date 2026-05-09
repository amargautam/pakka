package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/compress/semantic"
)

// --- State unit tests ---

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewState()
	s.Record("/x/CLAUDE.md", "ultra", "abc", "", "2026-04-29T00:00:00Z", true)
	s.Record("/x/DESIGN.md", "strict", "def", "", "2026-04-29T00:00:01Z", false)
	if err := s.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dir, ".pakka", StateFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	s2, err := LoadState(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s2.Save(dir); err != nil {
		t.Fatalf("save2: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(dir, ".pakka", StateFileName))
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("byte-stable round-trip violated:\n%s\n---\n%s", first, second)
	}
	if e, ok := s2.Get("/x/CLAUDE.md"); !ok || e.Level != "ultra" {
		t.Errorf("expected ultra entry, got %+v ok=%v", e, ok)
	}
	if s2.CountStale() != 1 {
		t.Errorf("CountStale want 1, got %d", s2.CountStale())
	}
}

func TestStaleMatrix(t *testing.T) {
	s := NewState()
	s.Record("/a", "strict", "sha-1", "", "t", true)
	s.Record("/b", "strict", "sha-1", "", "t", false)

	cases := []struct {
		name      string
		path      string
		level     string
		sha       string
		wantStale bool
	}{
		{"fresh-path", "/missing", "strict", "sha-1", true},
		{"level-changed", "/a", "ultra", "sha-1", true},
		{"sha-changed", "/a", "strict", "sha-2", true},
		{"prior-failure", "/b", "strict", "sha-1", true},
		{"all-current", "/a", "strict", "sha-1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := s.Stale(c.path, c.level, c.sha)
			if got != c.wantStale {
				t.Errorf("Stale(%s)=%v want %v", c.name, got, c.wantStale)
			}
		})
	}
}

// --- Orchestrator integration tests ---

// stubRewriter returns the supplied output (and validator-pass status). When
// failOnce > 0, the first call returns a *FailedError instead.
type stubRewriter struct {
	out      string
	calls    atomic.Int64
	delayMS  int
	fail     bool
	mu       sync.Mutex
	lastSeen string
}

func (s *stubRewriter) Rewrite(_ context.Context, input string, _ semantic.Level) (string, error) {
	s.calls.Add(1)
	s.mu.Lock()
	s.lastSeen = input
	s.mu.Unlock()
	if s.delayMS > 0 {
		time.Sleep(time.Duration(s.delayMS) * time.Millisecond)
	}
	if s.fail {
		// Drop a load-bearing region so validator fails.
		return "(redacted)", nil
	}
	return s.out, nil
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOrchestratorCompressesAndSkipsWhenUpToDate(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	body := "# Title\n\nSome filler text that is definitely longer than the rewrite output."
	writeFile(t, src, body)

	rew := &stubRewriter{out: "# Title\nshort"}
	var logBuf bytes.Buffer
	o := &Orchestrator{
		Repo:      repo,
		Targets:   []string{"CLAUDE.md"},
		Level:     "strict",
		SessionID: "tst00001",
		Rewriter:  rew,
		LogWriter: &logBuf,
	}

	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run1: %v", err)
	}
	got, _ := os.ReadFile(src)
	if string(got) != "# Title\nshort" {
		t.Fatalf("compressed body unexpected: %q", got)
	}
	// .original.md preserved.
	orig, err := os.ReadFile(filepath.Join(repo, "CLAUDE.original.md"))
	if err != nil || string(orig) != body {
		t.Fatalf("original missing or altered: %v %q", err, orig)
	}
	if rew.calls.Load() != 1 {
		t.Fatalf("rewriter calls want 1 got %d", rew.calls.Load())
	}

	// Second run: state up-to-date → no rewrite.
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if rew.calls.Load() != 1 {
		t.Fatalf("rewriter must not be called when up to date; got %d", rew.calls.Load())
	}
	if !strings.Contains(logBuf.String(), "up to date") {
		t.Errorf("expected 'up to date' log; got %q", logBuf.String())
	}

	// Level change → re-compress observed.
	o.Level = "ultra"
	rew.out = "# Title\nu"
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run3: %v", err)
	}
	if rew.calls.Load() != 2 {
		t.Fatalf("level change must re-compress; calls=%d", rew.calls.Load())
	}
	got, _ = os.ReadFile(src)
	if string(got) != "# Title\nu" {
		t.Errorf("level-change body wrong: %q", got)
	}
}

func TestOrchestratorLockContention(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	body := "# Title\n\nLong original body to compress."
	writeFile(t, src, body)

	// Each rewriter has a small delay so we get genuine contention on a
	// machine with multiple cores.
	rew1 := &stubRewriter{out: "# Title\nA", delayMS: 50}
	rew2 := &stubRewriter{out: "# Title\nB", delayMS: 50}

	o1 := &Orchestrator{Repo: repo, Targets: []string{"CLAUDE.md"}, Level: "strict", Rewriter: rew1, SessionID: "s1", LogWriter: &bytes.Buffer{}}
	o2 := &Orchestrator{Repo: repo, Targets: []string{"CLAUDE.md"}, Level: "strict", Rewriter: rew2, SessionID: "s2", LogWriter: &bytes.Buffer{}}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = o1.Run(context.Background()) }()
	go func() { defer wg.Done(); _ = o2.Run(context.Background()) }()
	wg.Wait()

	totalCalls := rew1.calls.Load() + rew2.calls.Load()
	if totalCalls != 1 {
		t.Errorf("exactly one orchestrator must compress under contention; total calls=%d", totalCalls)
	}
}

func TestOrchestratorValidatorFailureNoFileWrite(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	body := "# Title\n\nSee `loadBearing()` and https://pakka.dev for details."
	writeFile(t, src, body)

	// The stub drops the inline-code + URL load-bearing region — validator
	// will fail, RunSemantic returns the original input plus *FailedError.
	rew := &stubRewriter{fail: true}
	logBuf := &bytes.Buffer{}
	o := &Orchestrator{
		Repo: repo, Targets: []string{"CLAUDE.md"}, Level: "strict",
		Rewriter: rew, SessionID: "tstFail1", LogWriter: logBuf,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, _ := os.ReadFile(src)
	if string(got) != body {
		t.Errorf("file must remain untouched on validator failure; got %q", got)
	}
	if !strings.Contains(logBuf.String(), "validator-failed") {
		t.Errorf("missing validator-failed log: %q", logBuf.String())
	}
	st, _ := LoadState(repo)
	abs, _ := filepath.Abs(src)
	e, ok := st.Get(abs)
	if !ok {
		t.Fatalf("state must record failure")
	}
	if e.ValidatorPasses {
		t.Errorf("expected ValidatorPasses=false; got %+v", e)
	}
	if st.CountStale() != 1 {
		t.Errorf("CountStale want 1, got %d", st.CountStale())
	}
}

func TestOrchestratorEligibility(t *testing.T) {
	repo := t.TempDir()
	// Outside-repo escape attempt.
	o := &Orchestrator{Repo: repo, Targets: []string{"../escape.md"}, Level: "strict",
		Rewriter: &stubRewriter{out: "x"}, SessionID: "sec", LogWriter: &bytes.Buffer{}}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Symlink rejection.
	target := filepath.Join(repo, "real.md")
	link := filepath.Join(repo, "link.md")
	writeFile(t, target, "x")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink unsupported")
	}
	rew := &stubRewriter{out: "y"}
	o2 := &Orchestrator{Repo: repo, Targets: []string{"link.md"}, Level: "strict",
		Rewriter: rew, SessionID: "sec2", LogWriter: &bytes.Buffer{}}
	if err := o2.Run(context.Background()); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if rew.calls.Load() != 0 {
		t.Errorf("symlink must not be compressed; calls=%d", rew.calls.Load())
	}
}

// TestAsyncCommandConstruction asserts the fork command shape without forking.
func TestAsyncCommandConstruction(t *testing.T) {
	repo := t.TempDir()
	o := &Orchestrator{Repo: repo, Level: "ultra", SessionID: "s"}
	cmd := o.AsyncCommand()
	if cmd == nil {
		t.Fatalf("AsyncCommand returned nil")
	}
	wantArgs := map[string]bool{
		"compress":           true,
		"--orchestrator-bg":  true,
		"--level=ultra":      true,
		"--repo=" + repo:     true,
	}
	for _, a := range cmd.Args[1:] {
		if !wantArgs[a] {
			t.Errorf("unexpected arg %q (full=%v)", a, cmd.Args)
		}
		delete(wantArgs, a)
	}
	for missing := range wantArgs {
		t.Errorf("missing arg: %q", missing)
	}
	if cmd.Dir != repo {
		t.Errorf("Dir=%q want %q", cmd.Dir, repo)
	}
	if cmd.Stdin != nil {
		t.Errorf("Stdin must be nil for detached child")
	}
}

func TestOrchestratorReturnQuicklyWithLargeAllowlist(t *testing.T) {
	// Exercise the synchronous Run with many small files — must complete
	// in a few hundred ms with the stub. Behavioral guard: a regression that
	// added per-file network sleeps would blow this budget.
	repo := t.TempDir()
	var targets []string
	for i := 0; i < 12; i++ {
		name := filepath.Join("memory", "F"+string(rune('A'+i))+".md")
		writeFile(t, filepath.Join(repo, name), "# Hello\nbody body body")
		targets = append(targets, name)
	}
	rew := &stubRewriter{out: "# Hello\nx"}
	o := &Orchestrator{Repo: repo, Targets: targets, Level: "strict",
		Rewriter: rew, SessionID: "fast", LogWriter: &bytes.Buffer{}}
	start := time.Now()
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Run too slow with stub: %v", d)
	}
	if rew.calls.Load() != int64(len(targets)) {
		t.Errorf("calls=%d want %d", rew.calls.Load(), len(targets))
	}
}

func TestRunAsyncForkReturnsImmediately(t *testing.T) {
	// We cannot exec `pakka-core` from the test binary, but we can verify
	// AsyncCommand's intent: stdin is nil and detach attrs are configured.
	o := &Orchestrator{Repo: t.TempDir(), Level: "strict"}
	cmd := o.AsyncCommand()
	if cmd == nil {
		t.Skip("no executable in test binary; AsyncCommand returned nil (acceptable)")
	}
	if cmd.SysProcAttr == nil {
		t.Errorf("expected SysProcAttr set for detach")
	}
}

// TestStateBytesNotInLog is a smoke test: file content (which may contain
// secrets) must NEVER appear in the orchestrator log.
func TestStateBytesNotInLog(t *testing.T) {
	repo := t.TempDir()
	secret := "API_KEY=sk-DEADBEEF-not-real-still-do-not-leak"
	src := filepath.Join(repo, "CLAUDE.md")
	writeFile(t, src, "# Title\n"+secret+"\n")
	logBuf := &bytes.Buffer{}
	o := &Orchestrator{Repo: repo, Targets: []string{"CLAUDE.md"}, Level: "strict",
		Rewriter: &stubRewriter{out: "# Title\nshort"}, SessionID: "log",
		LogWriter: logBuf}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(logBuf.String(), secret) {
		t.Errorf("log leaked file content: %q", logBuf.String())
	}
}

// TestCountStaleFromDisk verifies the disk-only helper used by status-line.
func TestCountStaleFromDisk(t *testing.T) {
	repo := t.TempDir()
	// No state file → 0.
	if got := CountStaleFromDisk(repo); got != 0 {
		t.Errorf("missing state want 0 got %d", got)
	}
	// Bad JSON → 0 (never block status-line).
	dir := filepath.Join(repo, ".pakka")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, StateFileName), []byte("{not-json"), 0o644)
	if got := CountStaleFromDisk(repo); got != 0 {
		t.Errorf("bad json want 0 got %d", got)
	}
	// Good state with 2 failures.
	s := NewState()
	s.Record("/a", "strict", "x", "", "t", false)
	s.Record("/b", "strict", "y", "", "t", false)
	s.Record("/c", "strict", "z", "", "t", true)
	if err := s.Save(repo); err != nil {
		t.Fatal(err)
	}
	if got := CountStaleFromDisk(repo); got != 2 {
		t.Errorf("CountStaleFromDisk want 2 got %d", got)
	}
}

// TestUserEditPreservedOnLevelChange ensures that when a user edits the live
// compressed file and the orchestrator runs at a new level, the user edits are
// adopted as the new baseline rather than being silently overwritten.
func TestUserEditPreservedOnLevelChange(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	original := "# Title\n\nOriginal body that is definitely longer than any rewrite output here."
	writeFile(t, src, original)

	// Level A: compress the file.
	rewA := &stubRewriter{out: "# Title\ncompressed-A"}
	var logBuf bytes.Buffer
	o := &Orchestrator{
		Repo:      repo,
		Targets:   []string{"CLAUDE.md"},
		Level:     "strict",
		SessionID: "editTest1",
		Rewriter:  rewA,
		LogWriter: &logBuf,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run A: %v", err)
	}
	abs, _ := filepath.Abs(src)
	got, _ := os.ReadFile(abs)
	if string(got) != "# Title\ncompressed-A" {
		t.Fatalf("after level-A compress, expected compressed output, got %q", got)
	}

	// User edits the live file.
	userEdited := "# Title\n\nUser edited content with extra detail added manually."
	writeFile(t, src, userEdited)

	// Level B: run at a new level — orchestrator must use user-edited content
	// as compression input (not the original snapshot).
	rewB := &stubRewriter{out: "# Title\ncompressed-B"}
	o.Level = "ultra"
	o.Rewriter = rewB
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run B: %v", err)
	}

	// The rewriter must have seen the user-edited content as input.
	rewB.mu.Lock()
	seen := rewB.lastSeen
	rewB.mu.Unlock()
	if seen != userEdited {
		t.Errorf("rewriter input: got %q, want user-edited content %q", seen, userEdited)
	}
}

// TestNoEditNoSnapshotRefresh verifies that when the live file has NOT been
// edited after compression, a level change does NOT trigger a snapshot refresh.
func TestNoEditNoSnapshotRefresh(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	original := "# Title\n\nOriginal body that is definitely longer than any rewrite output here."
	writeFile(t, src, original)

	// Level A: compress.
	rewA := &stubRewriter{out: "# Title\ncompressed-A"}
	var logBuf bytes.Buffer
	o := &Orchestrator{
		Repo:      repo,
		Targets:   []string{"CLAUDE.md"},
		Level:     "strict",
		SessionID: "noEditTest1",
		Rewriter:  rewA,
		LogWriter: &logBuf,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run A: %v", err)
	}

	// Read the snapshot BEFORE level-B run.
	origPath := strings.TrimSuffix(src, ".md") + ".original.md"
	snapBefore, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	// Level B: no user edits — the live file still contains "# Title\ncompressed-A".
	rewB := &stubRewriter{out: "# Title\ncompressed-B"}
	o.Level = "ultra"
	o.Rewriter = rewB
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run B: %v", err)
	}

	// Snapshot must remain unchanged (no spurious refresh).
	snapAfter, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("read snapshot after: %v", err)
	}
	if string(snapAfter) != string(snapBefore) {
		t.Errorf("snapshot was spuriously refreshed: before=%q after=%q", snapBefore, snapAfter)
	}
}

// TestLegacyStateNoOutputSHA verifies that entries without an outputSHA field
// (legacy state) do NOT trigger a snapshot refresh — empty outputSHA is a safe
// no-op fallback.
func TestLegacyStateNoOutputSHA(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	original := "# Title\n\nOriginal body that is definitely longer than any rewrite output here."
	writeFile(t, src, original)

	// Manually write a legacy state entry (no outputSHA field).
	origPath := strings.TrimSuffix(src, ".md") + ".original.md"
	writeFile(t, origPath, original)
	abs, _ := filepath.Abs(src)
	sourceSHA := sha256Hex([]byte(original))

	s := NewState()
	// Record with empty outputSHA (legacy — 4th positional arg is "").
	s.Record(abs, "strict", sourceSHA, "", "2026-01-01T00:00:00Z", true)
	if err := s.Save(repo); err != nil {
		t.Fatalf("save state: %v", err)
	}

	// Simulate user editing the live file after the legacy run.
	userEdited := "# Title\n\nUser added something important here."
	writeFile(t, src, userEdited)

	// Run at a new level. Legacy entry (outputSHA="") must NOT cause refresh.
	// Because outputSHA is empty, detection is skipped → snapshot stays original.
	rewB := &stubRewriter{out: "# Title\ncompressed-B"}
	var logBuf bytes.Buffer
	o := &Orchestrator{
		Repo:      repo,
		Targets:   []string{"CLAUDE.md"},
		Level:     "ultra",
		SessionID: "legacyTest1",
		Rewriter:  rewB,
		LogWriter: &logBuf,
	}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Snapshot must remain as original (no refresh triggered by legacy entry).
	snapAfter, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if string(snapAfter) != original {
		t.Errorf("legacy state caused spurious snapshot refresh: got %q, want %q", snapAfter, original)
	}
}

// stubRewriter must satisfy semantic.Rewriter.
var _ semantic.Rewriter = (*stubRewriter)(nil)

// errAlways forces a non-FailedError path for one test.
var errAlways = errors.New("always fails")

// useAlwaysErrRewriter exercises the non-validator error path.
type alwaysErrRewriter struct{}

func (alwaysErrRewriter) Rewrite(context.Context, string, semantic.Level) (string, error) {
	return "", errAlways
}

// TestOrchestratorRewriterError — transient (non-FailedError) rewrite errors
// must NOT record state. File must remain untouched; state must have no entry.
func TestOrchestratorRewriterError(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	body := "# Title\nbody"
	writeFile(t, src, body)
	o := &Orchestrator{Repo: repo, Targets: []string{"CLAUDE.md"}, Level: "strict",
		Rewriter: alwaysErrRewriter{}, SessionID: "err", LogWriter: &bytes.Buffer{}}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, _ := os.ReadFile(src)
	if string(got) != body {
		t.Errorf("file must not change on rewriter error; got %q", got)
	}
	// Fix E: transient errors leave state unchanged — no entry should exist.
	st, _ := LoadState(repo)
	abs, _ := filepath.Abs(src)
	if _, ok := st.Get(abs); ok {
		t.Errorf("transient error must NOT record state entry; got entry for %s", abs)
	}
}

// TestTransientErrorPreservesPriorState — Fix E regression guard.
// A prior successful entry must survive a subsequent transient rewrite error
// (validatorPasses=true must remain, not be overwritten to false).
func TestTransientErrorPreservesPriorState(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	body := "# Title\n`loadBearing()` body longer than any rewrite"
	writeFile(t, src, body)

	// First run: succeeds and records validatorPasses=true.
	rew := &stubRewriter{out: "# Title\n`loadBearing()` x"}
	o := &Orchestrator{Repo: repo, Targets: []string{"CLAUDE.md"}, Level: "strict",
		Rewriter: rew, SessionID: "transient1", LogWriter: &bytes.Buffer{}}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run1: %v", err)
	}
	abs, _ := filepath.Abs(src)
	st, _ := LoadState(repo)
	e, ok := st.Get(abs)
	if !ok || !e.ValidatorPasses {
		t.Fatalf("expected successful first run; e=%+v ok=%v", e, ok)
	}
	priorSHA := e.SourceSHA
	priorTS := e.CompressedAt

	// Force file to be re-read as original so next run sees it as stale.
	// Delete the .original.md so the next run re-baselines from current file.
	_ = os.Remove(src + ".original.md")
	// Write a changed file so SHA differs → stale.
	writeFile(t, src, body+" changed")

	// Second run: transient error.
	o.Rewriter = alwaysErrRewriter{}
	if err := o.Run(context.Background()); err != nil {
		t.Fatalf("run2: %v", err)
	}

	// Prior entry must be unchanged.
	st2, _ := LoadState(repo)
	e2, ok2 := st2.Get(abs)
	if !ok2 {
		t.Fatalf("prior state entry must not be removed by transient error")
	}
	if !e2.ValidatorPasses {
		t.Errorf("transient error must not flip ValidatorPasses to false; e=%+v", e2)
	}
	if e2.SourceSHA != priorSHA {
		t.Errorf("SourceSHA must not change on transient error: want %q got %q", priorSHA, e2.SourceSHA)
	}
	if e2.CompressedAt != priorTS {
		t.Errorf("CompressedAt must not change on transient error")
	}
}

// Confirm that on second run after rewriter error, retry happens.
// Drives this through the rewriter-error path (FailedError handling has its
// own dedicated test above).
func TestOrchestratorRetriesPriorFailure(t *testing.T) {
	repo := t.TempDir()
	src := filepath.Join(repo, "CLAUDE.md")
	writeFile(t, src, "# T\n`loadBearing()` body")

	flaky := &flakyRewriter{out: "# T\n`loadBearing()` x"}
	o := &Orchestrator{Repo: repo, Targets: []string{"CLAUDE.md"}, Level: "strict",
		Rewriter: flaky, SessionID: "flake", LogWriter: &bytes.Buffer{}}
	_ = o.Run(context.Background())
	st, _ := LoadState(repo)
	abs, _ := filepath.Abs(src)
	if e, ok := st.Get(abs); !ok || e.ValidatorPasses {
		t.Fatalf("expected first run to fail (recorded as ValidatorPasses=false); got ok=%v e=%+v", ok, e)
	}
	flaky.healed.Store(true)
	// Second run should retry (Stale==true because ValidatorPasses=false).
	_ = o.Run(context.Background())
	st, _ = LoadState(repo)
	if e, _ := st.Get(abs); !e.ValidatorPasses {
		t.Errorf("retry should have succeeded; e=%+v", e)
	}
}

// flakyRewriter fails until healed flips, then returns out.
type flakyRewriter struct {
	out    string
	calls  atomic.Int64
	healed atomic.Bool
}

func (f *flakyRewriter) Rewrite(_ context.Context, _ string, _ semantic.Level) (string, error) {
	f.calls.Add(1)
	if !f.healed.Load() {
		// Drop the inline-code region — validator's KindInlineCodeChanged fires.
		return "# T\n(dropped) x", nil
	}
	return f.out, nil
}

// TestSavedStateJSON ensures the on-disk format matches the documented spec.
func TestSavedStateJSON(t *testing.T) {
	repo := t.TempDir()
	s := NewState()
	s.Record("/abs/CLAUDE.md", "ultra", "deadbeef", "", "2026-04-29T12:34:56Z", true)
	if err := s.Save(repo); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".pakka", StateFileName))
	var raw map[string]map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	e := raw["/abs/CLAUDE.md"]
	for _, k := range []string{"sourceSHA", "level", "compressedAt", "validatorPasses"} {
		if _, ok := e[k]; !ok {
			t.Errorf("missing field %q in %v", k, e)
		}
	}
}
