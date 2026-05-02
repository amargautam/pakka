package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner is the test double for the runner interface. It returns a
// canned response and token count per call, optionally varying by mode.
type fakeRunner struct {
	responses map[string]string // mode -> response text
	tokens    map[string]int    // mode -> token count
	calls     []string          // record of mode invocations
	failOn    map[string]error  // mode -> error to return
}

func (f *fakeRunner) Run(ctx context.Context, mode, input string) (string, int, error) {
	f.calls = append(f.calls, mode)
	if e, ok := f.failOn[mode]; ok {
		return "", 0, e
	}
	return f.responses[mode], f.tokens[mode], nil
}

// writeFixtureCorpus writes a fixture corpus and seed files to dir and
// returns the path to corpus.json. The fixture mirrors the real corpus
// shape: seeded-bug entries with string-path expected, clean entries with
// string-path expected, and one real-pr entry with inline-object expected.
func writeFixtureCorpus(t *testing.T, dir string, count int) string {
	t.Helper()

	type seed struct {
		id       string
		kind     string
		bug      string
		prompt   string
		diff     string
		expected string
	}

	seeds := []seed{
		{
			id: "seed-01", kind: "seeded-bug", bug: "n-plus-1-query",
			prompt:   "Review this for N+1 issues.",
			diff:     "+ for u := range users { db.Query(u) }",
			expected: `{"kind":"correctness","severity":"error","file":"users.go","line_approx":18,"bug_class":"n-plus-1-query","description":"loop query"}`,
		},
		{
			id: "seed-02", kind: "seeded-bug", bug: "null-deref",
			prompt:   "Look for nil derefs.",
			diff:     "+ x := *p // p might be nil",
			expected: `{"kind":"correctness","severity":"error","file":"main.go","line_approx":42,"bug_class":"null-deref","description":"nil deref"}`,
		},
		{
			id: "clean-01", kind: "clean",
			prompt:   "Review this clean code.",
			diff:     "+ // refactor: rename var",
			expected: `{"kind":"none","expected_findings":0,"description":"clean"}`,
		},
		{
			id: "real-01", kind: "real-pr",
			prompt:   "Review this real PR.",
			diff:     "+ doc fix",
			expected: ``, // inline object below
		},
		{
			id: "seed-03", kind: "seeded-bug", bug: "off-by-one",
			prompt:   "Off-by-one?",
			diff:     "+ for i := 0; i <= len(a); i++ {",
			expected: `{"kind":"correctness","severity":"error","file":"loop.go","line_approx":5,"bug_class":"off-by-one"}`,
		},
	}
	if count > len(seeds) {
		count = len(seeds)
	}
	seeds = seeds[:count]

	type corpusEntry struct {
		ID       string          `json:"id"`
		Kind     string          `json:"kind"`
		Language string          `json:"language"`
		BugClass string          `json:"bug_class,omitempty"`
		Diff     string          `json:"diff"`
		Prompt   string          `json:"prompt"`
		Expected json.RawMessage `json:"expected"`
	}

	var corpus []corpusEntry
	for _, s := range seeds {
		seedDir := filepath.Join(dir, "seeds", s.id)
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "prompt.md"), []byte(s.prompt), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "seed.patch"), []byte(s.diff), 0644); err != nil {
			t.Fatal(err)
		}

		var exp json.RawMessage
		if s.kind == "real-pr" {
			// Inline object form, like real-pr entries in the live corpus.
			exp = json.RawMessage(`{"should_block":false}`)
		} else {
			expPath := filepath.Join(seedDir, "expected.json")
			if err := os.WriteFile(expPath, []byte(s.expected), 0644); err != nil {
				t.Fatal(err)
			}
			// Reference via path (string).
			exp = json.RawMessage(fmt.Sprintf("%q", "seeds/"+s.id+"/expected.json"))
		}
		corpus = append(corpus, corpusEntry{
			ID:       s.id,
			Kind:     s.kind,
			Language: "go",
			BugClass: s.bug,
			Diff:     "seeds/" + s.id + "/seed.patch",
			Prompt:   "seeds/" + s.id + "/prompt.md",
			Expected: exp,
		})
	}

	path := filepath.Join(dir, "corpus.json")
	data, err := json.MarshalIndent(corpus, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Test 1: corpus loader parses entries and loadEntryBodies resolves
// prompt/diff paths to readable files. Content matches what was written.
func TestLoadCorpus_resolvesPathsAndReadsContent(t *testing.T) {
	dir := t.TempDir()
	corpusPath := writeFixtureCorpus(t, dir, 3)

	entries, corpusDir, err := LoadCorpus(corpusPath)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if got, want := len(entries), 3; got != want {
		t.Fatalf("entry count: got %d want %d", got, want)
	}
	if corpusDir != dir {
		t.Errorf("corpusDir: got %q want %q", corpusDir, dir)
	}

	// Bodies are loaded lazily — verify loadEntryBodies populates them.
	for i := range entries {
		if err := loadEntryBodies(&entries[i], corpusDir); err != nil {
			t.Fatalf("loadEntryBodies %s: %v", entries[i].ID, err)
		}
	}

	// First entry's prompt should match what the fixture wrote.
	if entries[0].PromptBody != "Review this for N+1 issues." {
		t.Errorf("prompt body mismatch: got %q", entries[0].PromptBody)
	}
	if !strings.Contains(entries[0].DiffBody, "db.Query(u)") {
		t.Errorf("diff body mismatch: got %q", entries[0].DiffBody)
	}
	// And the prompt path is resolved relative to corpus.json's directory.
	if !strings.HasPrefix(entries[0].PromptPath, dir) {
		t.Errorf("prompt path not resolved: %q", entries[0].PromptPath)
	}
}

// TestLoadCorpus_pathFallback covers the live-corpus mixed convention: one
// entry uses corpus-dir-relative paths (the seeded form), another uses
// parent-dir-relative paths (the real-PR form). Both must load.
func TestLoadCorpus_pathFallback(t *testing.T) {
	parent := t.TempDir()
	corpusDir := filepath.Join(parent, "benchmarks")
	if err := os.MkdirAll(corpusDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Entry A: corpus-dir-relative ("seeds/a/...") — files live under corpusDir.
	seedADir := filepath.Join(corpusDir, "seeds", "a")
	if err := os.MkdirAll(seedADir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedADir, "prompt.md"), []byte("PROMPT-A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedADir, "seed.patch"), []byte("DIFF-A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedADir, "expected.json"), []byte(`{"kind":"none"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Entry B: parent-dir-relative ("benchmarks/prompts/...") — files live
	// under corpusDir but are addressed via the repo-root-relative path.
	prDir := filepath.Join(corpusDir, "prompts")
	diffDir := filepath.Join(corpusDir, "diffs")
	if err := os.MkdirAll(prDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(diffDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prDir, "b.md"), []byte("PROMPT-B"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diffDir, "b.patch"), []byte("DIFF-B"), 0644); err != nil {
		t.Fatal(err)
	}

	corpus := []map[string]interface{}{
		{
			"id":       "a",
			"kind":     "seeded-bug",
			"language": "go",
			"prompt":   "seeds/a/prompt.md",
			"diff":     "seeds/a/seed.patch",
			"expected": "seeds/a/expected.json",
		},
		{
			"id":       "b",
			"kind":     "real-pr",
			"language": "go",
			"prompt":   "benchmarks/prompts/b.md",
			"diff":     "benchmarks/diffs/b.patch",
			"expected": map[string]interface{}{"should_block": false},
		},
	}
	corpusPath := filepath.Join(corpusDir, "corpus.json")
	data, err := json.Marshal(corpus)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corpusPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	entries, cdir, err := LoadCorpus(corpusPath)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	for i := range entries {
		if err := loadEntryBodies(&entries[i], cdir); err != nil {
			t.Fatalf("loadEntryBodies %s: %v", entries[i].ID, err)
		}
	}

	if entries[0].PromptBody != "PROMPT-A" || entries[0].DiffBody != "DIFF-A" {
		t.Errorf("entry a: bodies wrong: prompt=%q diff=%q", entries[0].PromptBody, entries[0].DiffBody)
	}
	if entries[1].PromptBody != "PROMPT-B" || entries[1].DiffBody != "DIFF-B" {
		t.Errorf("entry b: bodies wrong: prompt=%q diff=%q", entries[1].PromptBody, entries[1].DiffBody)
	}
}

// TestLoadCorpus_bothPathsMissing — when neither base resolves, the error
// message must cite both attempted paths so the contributor knows what was
// tried.
func TestLoadCorpus_bothPathsMissing(t *testing.T) {
	parent := t.TempDir()
	corpusDir := filepath.Join(parent, "benchmarks")
	if err := os.MkdirAll(corpusDir, 0755); err != nil {
		t.Fatal(err)
	}

	corpus := []map[string]interface{}{{
		"id":       "ghost",
		"kind":     "seeded-bug",
		"language": "go",
		"prompt":   "does/not/exist.md",
		"diff":     "does/not/exist.patch",
		"expected": map[string]interface{}{"should_block": false},
	}}
	corpusPath := filepath.Join(corpusDir, "corpus.json")
	data, _ := json.Marshal(corpus)
	if err := os.WriteFile(corpusPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	entries, cdir, err := LoadCorpus(corpusPath)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	err = loadEntryBodies(&entries[0], cdir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	primary := filepath.Join(corpusDir, "does/not/exist.md")
	fallback := filepath.Join(parent, "does/not/exist.md")
	if !strings.Contains(msg, primary) {
		t.Errorf("error missing primary path %q: %s", primary, msg)
	}
	if !strings.Contains(msg, fallback) {
		t.Errorf("error missing fallback path %q: %s", fallback, msg)
	}
}

// TestRun_limitFlagBeforeIO — corpus has 3 entries; entry 3's prompt path
// does not exist on disk. With --limit=2 the run completes successfully
// because the loader never tries to read entry 3.
func TestRun_limitFlagBeforeIO(t *testing.T) {
	dir := t.TempDir()

	// Hand-roll a corpus where entry 3's prompt file is missing on disk.
	// Entries 1 and 2 have real files; entry 3 references a nonexistent path.
	for _, id := range []string{"e1", "e2"} {
		seedDir := filepath.Join(dir, "seeds", id)
		if err := os.MkdirAll(seedDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "prompt.md"), []byte("p-"+id), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "seed.patch"), []byte("d-"+id), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(seedDir, "expected.json"), []byte(`{"kind":"none"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	corpus := []map[string]interface{}{
		{"id": "e1", "kind": "clean", "language": "go",
			"prompt": "seeds/e1/prompt.md", "diff": "seeds/e1/seed.patch",
			"expected": "seeds/e1/expected.json"},
		{"id": "e2", "kind": "clean", "language": "go",
			"prompt": "seeds/e2/prompt.md", "diff": "seeds/e2/seed.patch",
			"expected": "seeds/e2/expected.json"},
		{"id": "e3-broken", "kind": "clean", "language": "go",
			"prompt": "seeds/e3/does-not-exist.md", "diff": "seeds/e3/missing.patch",
			"expected": "seeds/e3/missing.json"},
	}
	corpusPath := filepath.Join(dir, "corpus.json")
	data, _ := json.Marshal(corpus)
	if err := os.WriteFile(corpusPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "out", "results.json")
	fr := &fakeRunner{
		responses: map[string]string{"raw": "", "pakka": ""},
		tokens:    map[string]int{"raw": 1, "pakka": 1},
	}
	opts := Options{
		CorpusPath: corpusPath,
		OutPath:    outPath,
		Limit:      2,
		Mode:       "both",
		Timeout:    time.Second,
	}
	if err := run(opts, fr); err != nil {
		t.Fatalf("run with --limit=2 should succeed despite broken entry 3: %v", err)
	}

	var out Output
	d, _ := os.ReadFile(outPath)
	if err := json.Unmarshal(d, &out); err != nil {
		t.Fatal(err)
	}
	if out.EntryCount != 2 {
		t.Errorf("entry_count: got %d want 2", out.EntryCount)
	}
	for _, pe := range out.PerEntry {
		if pe.ID == "e3-broken" {
			t.Errorf("entry 3 should not have been processed")
		}
	}
}

// Test 2: ExtractFindings pulls only valid JSON-line findings out of mixed
// prose/JSON text, and returns an empty slice (no error) when there are none.
func TestExtractFindings(t *testing.T) {
	mixed := `Here's my review of the diff:

The first issue is a classic N+1 pattern.
{"kind":"correctness","severity":"error","file":"users.go","line_approx":18,"bug_class":"n-plus-1-query"}
Some more prose.
{"kind":"security","severity":"warn","file":"auth.go","line_approx":40,"bug_class":"toctou"}
And a closing remark.
`
	got := ExtractFindings(mixed)
	if len(got) != 2 {
		t.Fatalf("findings: got %d want 2 (%+v)", len(got), got)
	}
	if got[0].BugClass != "n-plus-1-query" || got[1].BugClass != "toctou" {
		t.Errorf("findings content wrong: %+v", got)
	}

	none := "no JSON here\njust a sentence.\n"
	if got := ExtractFindings(none); len(got) != 0 {
		t.Errorf("expected 0 findings, got %d", len(got))
	}

	empty := ""
	if got := ExtractFindings(empty); len(got) != 0 {
		t.Errorf("empty input: expected 0, got %d", len(got))
	}
}

// Test 3: hit detector for seeded bugs covers the three branches in spec:
// matching bug_class, matching file at nearby line, and same file far away.
func TestHitSeeded(t *testing.T) {
	exp := Expected{BugClass: "n-plus-1-query", File: "x.go", LineApprox: 18}

	// (a) Same bug_class — hit.
	if !HitSeeded([]Finding{{BugClass: "n-plus-1-query", File: "other.go", LineApprox: 99}}, exp) {
		t.Error("same bug_class should hit")
	}

	// (b) Different bug_class but same file at line 19 (within ±5) — hit.
	if !HitSeeded([]Finding{{BugClass: "race-condition", File: "x.go", LineApprox: 19}}, exp) {
		t.Error("same file within ±5 lines should hit")
	}

	// (c) Different bug_class, same file, far away — miss.
	if HitSeeded([]Finding{{BugClass: "race-condition", File: "x.go", LineApprox: 30}}, exp) {
		t.Error("same file far away should not hit")
	}

	// (d) No findings — miss.
	if HitSeeded(nil, exp) {
		t.Error("no findings should not hit")
	}

	// (e) File matches via basename even when paths differ.
	if !HitSeeded([]Finding{{BugClass: "other", File: "pkg/x.go", LineApprox: 17}}, exp) {
		t.Error("basename match should hit")
	}
}

// Test 4: hit detector for clean entries.
func TestHitClean(t *testing.T) {
	if !HitClean(nil) {
		t.Error("zero findings on clean should hit (no FP)")
	}
	if HitClean([]Finding{{Kind: "correctness", File: "x.go"}}) {
		t.Error("one finding on clean should miss (FP)")
	}
}

// Test 5: --limit caps the number of entries actually run.
func TestRun_limitFlag(t *testing.T) {
	dir := t.TempDir()
	corpusPath := writeFixtureCorpus(t, dir, 5)
	outPath := filepath.Join(dir, "out", "results.json")

	fr := &fakeRunner{
		responses: map[string]string{"raw": "no findings\n", "pakka": "no findings\n"},
		tokens:    map[string]int{"raw": 100, "pakka": 80},
	}

	opts := Options{
		CorpusPath: corpusPath,
		OutPath:    outPath,
		Limit:      2,
		Mode:       "both",
		Timeout:    time.Second,
	}
	if err := run(opts, fr); err != nil {
		t.Fatalf("run: %v", err)
	}

	// 2 entries × 2 modes = 4 calls.
	if got, want := len(fr.calls), 4; got != want {
		t.Errorf("calls: got %d want %d", got, want)
	}

	var out Output
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.EntryCount != 2 {
		t.Errorf("entry_count: got %d want 2", out.EntryCount)
	}
	if len(out.PerEntry) != 2 {
		t.Errorf("per_entry length: got %d want 2", len(out.PerEntry))
	}
}

// Test 6: aggregation counts seeded hits and clean false positives correctly
// across a small mixed corpus.
func TestRun_aggregation(t *testing.T) {
	dir := t.TempDir()
	corpusPath := writeFixtureCorpus(t, dir, 3) // seed-01, seed-02, clean-01
	outPath := filepath.Join(dir, "results.json")

	// pakka response: 2 seeded hits + 1 clean false positive.
	pakkaResponse := strings.Join([]string{
		// matches seed-01 (n-plus-1-query) and seed-02 (null-deref) and
		// emits a finding on clean-01 (false positive). The same response
		// is returned for every call but the corpus loader iterates by
		// entry id, so the same fake response means: hit, hit, fp.
		`{"bug_class":"n-plus-1-query","file":"users.go","line_approx":18}`,
		`{"bug_class":"null-deref","file":"main.go","line_approx":42}`,
		`{"kind":"style","file":"misc.go","line_approx":1}`,
	}, "\n")

	// raw response: nothing — no hits, no FPs.
	rawResponse := "no findings here\n"

	fr := &fakeRunner{
		responses: map[string]string{"raw": rawResponse, "pakka": pakkaResponse},
		tokens:    map[string]int{"raw": 100, "pakka": 75},
	}

	opts := Options{
		CorpusPath: corpusPath,
		OutPath:    outPath,
		Mode:       "both",
		Timeout:    time.Second,
	}
	if err := run(opts, fr); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var out Output
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	pakkaSummary := out.Summary["pakka"]
	rawSummary := out.Summary["raw"]
	if pakkaSummary == nil || rawSummary == nil {
		t.Fatal("missing summaries")
	}

	if pakkaSummary.SeededTotal != 2 {
		t.Errorf("pakka seeded_total: got %d want 2", pakkaSummary.SeededTotal)
	}
	if pakkaSummary.SeededHitCount != 2 {
		t.Errorf("pakka seeded_hit_count: got %d want 2", pakkaSummary.SeededHitCount)
	}
	if pakkaSummary.CleanTotal != 1 {
		t.Errorf("pakka clean_total: got %d want 1", pakkaSummary.CleanTotal)
	}
	// The pakka response emits 3 findings; on clean-01 all 3 count as FPs.
	if pakkaSummary.FPCountClean != 3 {
		t.Errorf("pakka fp_count_clean: got %d want 3", pakkaSummary.FPCountClean)
	}
	if pakkaSummary.TokensTotal != 75*3 {
		t.Errorf("pakka tokens_total: got %d want %d", pakkaSummary.TokensTotal, 75*3)
	}

	if rawSummary.SeededHitCount != 0 {
		t.Errorf("raw seeded_hit_count: got %d want 0", rawSummary.SeededHitCount)
	}
	if rawSummary.FPCountClean != 0 {
		t.Errorf("raw fp_count_clean: got %d want 0", rawSummary.FPCountClean)
	}
}

// Test 7: output JSON matches the documented schema and `commit` is non-empty.
func TestRun_outputSchema(t *testing.T) {
	dir := t.TempDir()
	corpusPath := writeFixtureCorpus(t, dir, 1)
	outPath := filepath.Join(dir, "nested", "results.json")

	fr := &fakeRunner{
		responses: map[string]string{"raw": "", "pakka": ""},
		tokens:    map[string]int{"raw": 50, "pakka": 50},
	}

	opts := Options{CorpusPath: corpusPath, OutPath: outPath, Mode: "both", Timeout: time.Second}
	if err := run(opts, fr); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("nested out path not created: %v", err)
	}
	var out Output
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("schema: cannot unmarshal documented shape: %v", err)
	}
	if out.TS == "" {
		t.Error("ts is empty")
	}
	if out.Commit == "" {
		t.Error("commit is empty")
	}
	if out.CorpusPath != corpusPath {
		t.Errorf("corpus_path: got %q want %q", out.CorpusPath, corpusPath)
	}
	if got, want := out.Modes, []string{"raw", "pakka"}; !equalSlices(got, want) {
		t.Errorf("modes: got %v want %v", got, want)
	}
	if _, ok := out.Summary["raw"]; !ok {
		t.Error("missing raw summary")
	}
	if _, ok := out.Summary["pakka"]; !ok {
		t.Error("missing pakka summary")
	}
}

// Bonus: per-entry transport error is recorded but does not abort the run.
func TestRun_perEntryError(t *testing.T) {
	dir := t.TempDir()
	corpusPath := writeFixtureCorpus(t, dir, 2)
	outPath := filepath.Join(dir, "results.json")

	fr := &fakeRunner{
		responses: map[string]string{"raw": "ok", "pakka": "ok"},
		tokens:    map[string]int{"raw": 1, "pakka": 1},
		failOn:    map[string]error{"raw": fmt.Errorf("simulated timeout")},
	}

	opts := Options{CorpusPath: corpusPath, OutPath: outPath, Mode: "both", Timeout: time.Second}
	if err := run(opts, fr); err != nil {
		t.Fatalf("run should not abort on per-entry error: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	var out Output
	_ = json.Unmarshal(data, &out)
	for _, pe := range out.PerEntry {
		if pe.Raw == nil || pe.Raw.Error == "" {
			t.Errorf("entry %s: expected raw error, got %+v", pe.ID, pe.Raw)
		}
		if pe.Pakka == nil || pe.Pakka.Error != "" {
			t.Errorf("entry %s: expected pakka clean, got %+v", pe.ID, pe.Pakka)
		}
	}
}

// BuildInput sanity check.
func TestBuildInput(t *testing.T) {
	got := BuildInput("review this", "diff body")
	if !strings.Contains(got, "review this") || !strings.Contains(got, "```diff\ndiff body\n```") {
		t.Errorf("BuildInput shape wrong:\n%s", got)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
