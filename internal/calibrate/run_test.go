package calibrate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- test helpers ---

// makeSeedPatch builds a valid git patch that adds relFile with content by
// staging it in a scratch repo and capturing `git diff --cached`. This avoids
// hand-counting hunk headers.
func makeSeedPatch(t *testing.T, relFile, content string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	scratch := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "x@y.z"}, {"config", "user.name", "x"}} {
		if out, err := runGit(scratch, args...); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	full := filepath.Join(scratch, relFile)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(scratch, "add", "-A"); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	patch, err := runGit(scratch, "diff", "--cached")
	if err != nil {
		t.Fatalf("git diff --cached: %v", err)
	}
	return patch
}

// writeSeed writes a seed fixture under seedsDir/name.
func writeSeed(t *testing.T, seedsDir, name, patch, expected string) {
	t.Helper()
	dir := filepath.Join(seedsDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.patch"), []byte(patch), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "expected.json"), []byte(expected), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("Review the staged diff.\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// buildFixtureRoot lays down a repo root with one bug seed, one clean seed, and
// one reviewer agent file. Returns the root.
func buildFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	seedsDir := filepath.Join(root, "benchmarks", "seeds")

	bugContent := "package svc\n\nfunc Handler() {\n\tfor rows.Next() {\n\t\tquery()\n\t}\n}\n"
	writeSeed(t, seedsDir, "bug-01",
		makeSeedPatch(t, "svc/handler.go", bugContent),
		`{"kind":"correctness","severity":"error","file":"svc/handler.go","line_approx":5,"bug_class":"n-plus-1-query","description":"n+1"}`)

	cleanContent := "package svc\n\nfunc Clean() int {\n\treturn 1\n}\n"
	writeSeed(t, seedsDir, "clean-01",
		makeSeedPatch(t, "svc/clean.go", cleanContent),
		`{"kind":"none","expected_findings":0,"description":"clean"}`)

	agentsDir := filepath.Join(root, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	agent := "---\nname: reviewer\nmodel: opus\ntools: Read, Bash\n---\n\n## Instructions\nReview the diff.\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte(agent), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

// fakeRunner is an injected agentRunner. fn decides the response per call.
type fakeRunner struct {
	fn func(ctx context.Context, workdir, sys, user string) (string, string, error)
}

func (r *fakeRunner) Run(ctx context.Context, workdir, sys, user string) (string, string, error) {
	return r.fn(ctx, workdir, sys, user)
}

// stagedDiffContains reports whether the workdir's staged diff mentions s —
// used by the fake to decide which seed it is reviewing.
func stagedDiffContains(workdir, s string) bool {
	out, err := runGit(workdir, "diff", "--cached")
	if err != nil {
		return false
	}
	return strings.Contains(out, s)
}

// --- AC1: full run writes a scored artifact ---

func TestRun_writesArtifact(t *testing.T) {
	root := buildFixtureRoot(t)

	fake := &fakeRunner{fn: func(ctx context.Context, workdir, sys, user string) (string, string, error) {
		// The bug seed touches svc/handler.go — emit a matching finding plus a
		// junk prose line. The clean seed gets prose only (no finding).
		if stagedDiffContains(workdir, "svc/handler.go") {
			resp := "Here is my review:\n" +
				`{"kind":"correctness","file":"svc/handler.go","line":5,"severity":"error","confidence":90,"rationale":"n+1"}` + "\n"
			return resp, "claude-test-model", nil
		}
		return "No issues found.\n", "claude-test-model", nil
	}}

	opts := Options{
		RepoRoot:   root,
		AgentFiles: []string{"agents/reviewer.md"},
		Date:       "2026-07-27",
		runner:     fake,
		Stdout:     &strings.Builder{},
		Stderr:     &strings.Builder{},
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := filepath.Join(root, "benchmarks", "results", "calibration-2026-07-27.json")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("artifact not written: %v", err)
	}
	var art Artifact
	if err := json.Unmarshal(data, &art); err != nil {
		t.Fatalf("artifact malformed: %v", err)
	}

	if art.Threshold != 80 {
		t.Errorf("threshold=%d, want 80", art.Threshold)
	}
	if art.Model != "claude-test-model" {
		t.Errorf("model=%q, want claude-test-model", art.Model)
	}
	if len(art.Agents) != 1 || art.Agents[0].SHA256 == "" {
		t.Errorf("agents=%+v, want 1 with sha", art.Agents)
	}
	if len(art.Seeds) != 2 {
		t.Fatalf("seeds=%d, want 2", len(art.Seeds))
	}
	// Verify per-seed verdicts.
	bySeed := map[string]SeedResult{}
	for _, s := range art.Seeds {
		bySeed[s.Seed] = s
	}
	if !bySeed["bug-01"].Recalled {
		t.Errorf("bug-01 should be recalled")
	}
	if bySeed["bug-01"].FalsePositive {
		t.Errorf("bug-01 should not be a false positive")
	}
	if bySeed["clean-01"].FalsePositive {
		t.Errorf("clean-01 should not be a false positive (fake emitted no finding)")
	}
	if len(bySeed["bug-01"].Findings) != 1 {
		t.Errorf("bug-01 findings=%d, want 1 (junk line tolerated)", len(bySeed["bug-01"].Findings))
	}
	// Aggregate.
	if art.Aggregate.Recall != 1.0 {
		t.Errorf("recall=%v, want 1.0", art.Aggregate.Recall)
	}
	if art.Aggregate.Precision != 1.0 {
		t.Errorf("precision=%v, want 1.0", art.Aggregate.Precision)
	}
	if art.Aggregate.FPRate != 0.0 {
		t.Errorf("fpRate=%v, want 0.0", art.Aggregate.FPRate)
	}
	if art.Aggregate.N != 1 || art.Aggregate.NClean != 1 {
		t.Errorf("n=%d nClean=%d, want 1/1", art.Aggregate.N, art.Aggregate.NClean)
	}
	if art.Aggregate.Model != "claude-test-model" {
		t.Errorf("aggregate model=%q", art.Aggregate.Model)
	}
}

// A clean fixture that DOES draw a finding is scored as a false positive.
func TestRun_cleanFalsePositive(t *testing.T) {
	root := buildFixtureRoot(t)
	fake := &fakeRunner{fn: func(ctx context.Context, workdir, sys, user string) (string, string, error) {
		// Emit a finding for EVERY seed, including the clean one.
		if stagedDiffContains(workdir, "svc/clean.go") {
			return `{"kind":"correctness","file":"svc/clean.go","line":3,"severity":"warn","confidence":85,"rationale":"nit"}` + "\n", "m", nil
		}
		return `{"kind":"correctness","file":"svc/handler.go","line":5,"severity":"error","confidence":90,"rationale":"n+1"}` + "\n", "m", nil
	}}
	opts := Options{RepoRoot: root, AgentFiles: []string{"agents/reviewer.md"}, Date: "2026-07-27", runner: fake,
		Stdout: &strings.Builder{}, Stderr: &strings.Builder{}}
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "benchmarks", "results", "calibration-2026-07-27.json"))
	var art Artifact
	json.Unmarshal(data, &art)
	if art.Aggregate.FPRate != 1.0 {
		t.Errorf("fpRate=%v, want 1.0 (1 clean finding / 1 clean run)", art.Aggregate.FPRate)
	}
}

// --- Finding 1: timeout/error seeds are excluded from the rates ---

// buildTwoBugRoot lays down two bug seeds (no clean) + one agent file.
func buildTwoBugRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	seedsDir := filepath.Join(root, "benchmarks", "seeds")
	writeSeed(t, seedsDir, "bug-01",
		makeSeedPatch(t, "svc/handler.go", "package svc\n\nfunc H() {\n\tfor {\n\t\tq()\n\t}\n}\n"),
		`{"kind":"correctness","file":"svc/handler.go","line_approx":5,"bug_class":"n-plus-1-query"}`)
	writeSeed(t, seedsDir, "bug-02",
		makeSeedPatch(t, "svc/other.go", "package svc\n\nfunc O() {\n\tfor {\n\t\tp()\n\t}\n}\n"),
		`{"kind":"correctness","file":"svc/other.go","line_approx":5,"bug_class":"off-by-one"}`)
	agentsDir := filepath.Join(root, "agents")
	os.MkdirAll(agentsDir, 0755)
	os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte("---\nname: reviewer\n---\n\nReview.\n"), 0644)
	return root
}

func loadArtifact(t *testing.T, root, date string) Artifact {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "benchmarks", "results", "calibration-"+date+".json"))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var art Artifact
	if err := json.Unmarshal(data, &art); err != nil {
		t.Fatalf("unmarshal artifact: %v", err)
	}
	return art
}

// A seed that times out is recorded but excluded from the recall denominator,
// so an infrastructure failure never deflates the rate. The artifact discloses
// the timeout count.
func TestRun_timeoutExcludedFromRates(t *testing.T) {
	root := buildTwoBugRoot(t)
	fake := &fakeRunner{fn: func(ctx context.Context, workdir, sys, user string) (string, string, error) {
		if stagedDiffContains(workdir, "svc/handler.go") {
			// bug-01: matching finding, returns immediately.
			return `{"kind":"correctness","file":"svc/handler.go","line":5,"severity":"error","confidence":90,"rationale":"x"}` + "\n", "m", nil
		}
		// bug-02: hang until the per-seed deadline.
		<-ctx.Done()
		return "", "", ctx.Err()
	}}
	opts := Options{RepoRoot: root, AgentFiles: []string{"agents/reviewer.md"}, Date: "2026-07-27",
		SeedTimeout: 30 * time.Millisecond, runner: fake, Stdout: &strings.Builder{}, Stderr: &strings.Builder{}}
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	art := loadArtifact(t, root, "2026-07-27")

	// Recall denominator excludes the timed-out seed → 1/1 = 1.0, not 1/2.
	if art.Aggregate.Recall != 1.0 {
		t.Errorf("recall=%v, want 1.0 (timed-out seed excluded)", art.Aggregate.Recall)
	}
	if art.Aggregate.N != 1 {
		t.Errorf("n=%d, want 1 (only the scored bug seed)", art.Aggregate.N)
	}
	if art.Aggregate.Counts.Scored != 1 || art.Aggregate.Counts.Timeout != 1 {
		t.Errorf("counts=%+v, want scored=1 timeout=1", art.Aggregate.Counts)
	}
	// The timed-out seed is still in the artifact, flagged, not scored.
	var bug02 *SeedResult
	for i := range art.Seeds {
		if art.Seeds[i].Seed == "bug-02" {
			bug02 = &art.Seeds[i]
		}
	}
	if bug02 == nil || !bug02.Timeout || bug02.Scored {
		t.Errorf("bug-02 should be recorded with timeout=true scored=false, got %+v", bug02)
	}
}

// A seed whose materialization/transport errors is likewise excluded and counted.
func TestRun_errorExcludedFromRates(t *testing.T) {
	root := buildTwoBugRoot(t)
	fake := &fakeRunner{fn: func(ctx context.Context, workdir, sys, user string) (string, string, error) {
		if stagedDiffContains(workdir, "svc/handler.go") {
			return `{"kind":"correctness","file":"svc/handler.go","line":5,"severity":"error","confidence":90,"rationale":"x"}` + "\n", "m", nil
		}
		return "", "", errUnavailable
	}}
	opts := Options{RepoRoot: root, AgentFiles: []string{"agents/reviewer.md"}, Date: "2026-07-27",
		runner: fake, Stdout: &strings.Builder{}, Stderr: &strings.Builder{}}
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	art := loadArtifact(t, root, "2026-07-27")
	if art.Aggregate.Recall != 1.0 || art.Aggregate.N != 1 {
		t.Errorf("recall=%v n=%d, want 1.0/1 (errored seed excluded)", art.Aggregate.Recall, art.Aggregate.N)
	}
	if art.Aggregate.Counts.Error != 1 || art.Aggregate.Counts.Scored != 1 {
		t.Errorf("counts=%+v, want error=1 scored=1", art.Aggregate.Counts)
	}
}

var errUnavailable = errors.New("service unavailable")

// --- Finding 2: parse-failure degradation ---

// buildThreeBugRoot lays down three bug seeds + one agent file.
func buildThreeBugRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	seedsDir := filepath.Join(root, "benchmarks", "seeds")
	for _, n := range []string{"a", "b", "c"} {
		writeSeed(t, seedsDir, "bug-"+n,
			makeSeedPatch(t, "svc/"+n+".go", "package svc\n\nfunc F"+n+"() {\n\tfor {\n\t\tq()\n\t}\n}\n"),
			`{"kind":"correctness","file":"svc/`+n+`.go","line_approx":5,"bug_class":"n-plus-1-query"}`)
	}
	agentsDir := filepath.Join(root, "agents")
	os.MkdirAll(agentsDir, 0755)
	os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte("---\nname: reviewer\n---\n\nReview.\n"), 0644)
	return root
}

// When a majority of scored seeds return a non-empty response but zero parsed
// findings, the run is marked degraded and per-seed ParsedNothing is set.
func TestRun_degradedByParseFailure(t *testing.T) {
	root := buildThreeBugRoot(t)
	fake := &fakeRunner{fn: func(ctx context.Context, workdir, sys, user string) (string, string, error) {
		// Only bug-a yields a real JSON finding; b and c return prose (parse
		// nothing) → 2 of 3 scored seeds parsedNothing → degraded.
		if stagedDiffContains(workdir, "svc/a.go") {
			return `{"kind":"correctness","file":"svc/a.go","line":5,"severity":"error","confidence":90,"rationale":"x"}` + "\n", "m", nil
		}
		return "I reviewed the diff and everything looks reasonable to me.\n", "m", nil
	}}
	opts := Options{RepoRoot: root, AgentFiles: []string{"agents/reviewer.md"}, Date: "2026-07-27",
		runner: fake, Stdout: &strings.Builder{}, Stderr: &strings.Builder{}}
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	art := loadArtifact(t, root, "2026-07-27")
	if !art.Aggregate.Degraded {
		t.Errorf("expected Degraded=true (2/3 scored seeds parsed nothing)")
	}
	pn := 0
	for _, s := range art.Seeds {
		if s.ParsedNothing {
			pn++
		}
	}
	if pn != 2 {
		t.Errorf("parsedNothing seeds=%d, want 2", pn)
	}
}

// A minority of parse failures does NOT degrade the run.
func TestRun_notDegradedWhenMinorityParseNothing(t *testing.T) {
	root := buildThreeBugRoot(t)
	fake := &fakeRunner{fn: func(ctx context.Context, workdir, sys, user string) (string, string, error) {
		// Only bug-c parses nothing (1 of 3) → below the >50% bar.
		if stagedDiffContains(workdir, "svc/c.go") {
			return "Looks fine.\n", "m", nil
		}
		name := "a"
		if stagedDiffContains(workdir, "svc/b.go") {
			name = "b"
		}
		return `{"kind":"correctness","file":"svc/` + name + `.go","line":5,"severity":"error","confidence":90,"rationale":"x"}` + "\n", "m", nil
	}}
	opts := Options{RepoRoot: root, AgentFiles: []string{"agents/reviewer.md"}, Date: "2026-07-27",
		runner: fake, Stdout: &strings.Builder{}, Stderr: &strings.Builder{}}
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	art := loadArtifact(t, root, "2026-07-27")
	if art.Aggregate.Degraded {
		t.Errorf("expected Degraded=false (only 1/3 parsed nothing)")
	}
}

// Unit: the >50% boundary of the degradation predicate.
func TestDegradedByParseFailure_boundary(t *testing.T) {
	cases := []struct {
		pn, scored int
		want       bool
	}{
		{0, 0, false}, // no scored seeds
		{1, 2, false}, // exactly 50% is not a majority
		{2, 3, true},  // 66%
		{3, 3, true},  // all
		{1, 3, false}, // 33%
	}
	for _, c := range cases {
		if got := DegradedByParseFailure(c.pn, c.scored); got != c.want {
			t.Errorf("DegradedByParseFailure(%d,%d)=%v, want %v", c.pn, c.scored, got, c.want)
		}
	}
}

// --- AC4/AC5: skip when claude CLI absent, never fall back to an API key ---

func TestRun_skipsWhenClaudeAbsent(t *testing.T) {
	root := buildFixtureRoot(t)
	// An API key present must NOT enable a run — the harness never reads it.
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-be-ignored")

	opts := Options{
		RepoRoot:  root,
		ClaudeBin: "pakka-no-such-claude-binary-xyz",
		Date:      "2026-07-27",
		// runner nil → real path → LookPath fails → skip.
		Stdout: &strings.Builder{},
		Stderr: &strings.Builder{},
	}
	err := Run(opts)
	var skip *SkipError
	if !errors.As(err, &skip) {
		t.Fatalf("expected *SkipError, got %v", err)
	}
	// Nothing written.
	if _, statErr := os.Stat(filepath.Join(root, "benchmarks", "results", "calibration-2026-07-27.json")); !os.IsNotExist(statErr) {
		t.Errorf("skip must write no artifact, but one exists")
	}
}

// AC5: the calibrate source tree must contain no ANTHROPIC_API_KEY reference —
// there is no API-key code path. (Test files are excluded; this test names it.)
func TestNoAPIKeyInSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		data, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "ANTHROPIC_API_KEY") {
			t.Errorf("%s references ANTHROPIC_API_KEY — the calibrate harness must never read an API key", n)
		}
	}
}

// --- AC6: per-seed timeout marks the seed and the run continues ---

func TestRun_perSeedTimeout(t *testing.T) {
	root := buildFixtureRoot(t)
	fake := &fakeRunner{fn: func(ctx context.Context, workdir, sys, user string) (string, string, error) {
		// Block until the per-seed deadline fires.
		<-ctx.Done()
		return "", "", ctx.Err()
	}}
	opts := Options{
		RepoRoot:    root,
		AgentFiles:  []string{"agents/reviewer.md"},
		Date:        "2026-07-27",
		SeedTimeout: 20 * time.Millisecond,
		runner:      fake,
		Stdout:      &strings.Builder{},
		Stderr:      &strings.Builder{},
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run should complete despite timeouts: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "benchmarks", "results", "calibration-2026-07-27.json"))
	if err != nil {
		t.Fatalf("artifact should still be written: %v", err)
	}
	var art Artifact
	json.Unmarshal(data, &art)
	if len(art.Seeds) != 2 {
		t.Fatalf("run must continue past a timeout: seeds=%d, want 2", len(art.Seeds))
	}
	for _, s := range art.Seeds {
		if !s.Timeout {
			t.Errorf("seed %s expected timeout=true", s.Seed)
		}
	}
}

// --- unit: ParseFindings tolerates junk ---

func TestParseFindings_tolerateJunk(t *testing.T) {
	resp := "Some prose.\n" +
		`{"kind":"correctness","file":"a.go","line":1,"severity":"error","confidence":90,"rationale":"x"}` + "\n" +
		"not json {broken\n" +
		"{}\n" + // empty object — no meaningful field, dropped
		`{"kind":"security","file":"b.go","line":2,"severity":"error","confidence":95,"rationale":"y"}` + "\n"
	got := ParseFindings(resp)
	if len(got) != 2 {
		t.Fatalf("ParseFindings=%d, want 2: %+v", len(got), got)
	}
}

// --- unit: stripFrontmatter removes the YAML block ---

func TestStripFrontmatter(t *testing.T) {
	in := "---\nname: reviewer\nmodel: opus\n---\n\n## Instructions\nBody here.\n"
	got := stripFrontmatter(in)
	if strings.Contains(got, "name: reviewer") {
		t.Errorf("frontmatter not stripped: %q", got)
	}
	if !strings.Contains(got, "## Instructions") {
		t.Errorf("body lost: %q", got)
	}
	// No frontmatter → unchanged.
	plain := "just a body\n"
	if stripFrontmatter(plain) != plain {
		t.Errorf("plain body altered")
	}
}

// --- unit: materializeSeed stages the patch as a diff ---

func TestMaterializeSeed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	seedsDir := t.TempDir()
	patch := makeSeedPatch(t, "pkg/x.go", "package pkg\n\nfunc X() {}\n")
	writeSeed(t, seedsDir, "s1", patch, `{"kind":"correctness","file":"pkg/x.go","line_approx":3}`)

	repo, cleanup, err := materializeSeed(filepath.Join(seedsDir, "s1"))
	if err != nil {
		t.Fatalf("materializeSeed: %v", err)
	}
	defer cleanup()

	diff, err := runGit(repo, "diff", "--cached")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "pkg/x.go") {
		t.Errorf("staged diff missing the seed file:\n%s", diff)
	}
}

// Regression: materializeSeed must apply a seed patch given a RELATIVE seedDir
// from a working directory that is NOT the seed's location. git apply runs with
// cmd.Dir = temp repo, so a relative patch path used to be resolved against the
// temp dir and failed with exit 128. Mirrors production (calibrate --repo-root=.
// → seedDir "benchmarks/seeds/<seed>").
func TestMaterializeSeed_relativePathFromDifferentCWD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	seedsRel := filepath.Join("benchmarks", "seeds")
	writeSeed(t, filepath.Join(root, seedsRel), "s1",
		makeSeedPatch(t, "pkg/x.go", "package pkg\n\nfunc X() {}\n"),
		`{"kind":"correctness","file":"pkg/x.go","line_approx":3}`)

	t.Chdir(root) // CWD is the repo root; seedDir is passed relative to it.

	repo, cleanup, err := materializeSeed(filepath.Join(seedsRel, "s1"))
	if err != nil {
		t.Fatalf("materializeSeed with relative path failed: %v", err)
	}
	defer cleanup()
	diff, err := runGit(repo, "diff", "--cached")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "pkg/x.go") {
		t.Errorf("staged diff missing the seed file:\n%s", diff)
	}
}

// Regression (full call path): Run with RepoRoot="." from a different CWD must
// score seeds, not fail every one with git apply exit 128. This is the exact
// production invocation (make calibrate → calibrate --repo-root=.).
func TestRun_relativeRepoRootFromCWD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := buildFixtureRoot(t)
	t.Chdir(root)

	fake := &fakeRunner{fn: func(ctx context.Context, workdir, sys, user string) (string, string, error) {
		if stagedDiffContains(workdir, "svc/handler.go") {
			return `{"kind":"correctness","file":"svc/handler.go","line":5,"severity":"error","confidence":90,"rationale":"x"}` + "\n", "m", nil
		}
		return "No issues found.\n", "m", nil
	}}
	opts := Options{
		RepoRoot:   ".", // production shape
		AgentFiles: []string{"agents/reviewer.md"},
		Date:       "2026-07-27",
		runner:     fake,
		Stdout:     &strings.Builder{},
		Stderr:     &strings.Builder{},
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	art := loadArtifact(t, root, "2026-07-27")
	if art.Aggregate.Counts.Error != 0 {
		t.Fatalf("expected 0 errored seeds, got %d (git-apply-relative regression): seeds=%+v",
			art.Aggregate.Counts.Error, art.Seeds)
	}
	if art.Aggregate.Counts.Scored != 2 {
		t.Errorf("scored=%d, want 2", art.Aggregate.Counts.Scored)
	}
	if art.Aggregate.Recall != 1.0 {
		t.Errorf("recall=%v, want 1.0", art.Aggregate.Recall)
	}
}
