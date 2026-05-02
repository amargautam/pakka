// Package bench implements the v0 benchmark harness.
//
// It runs each corpus entry through two arms — raw `claude -p` (no pakka)
// and `claude -p` with the pakka plugin active — then compares the model's
// emitted JSON-line findings to per-entry expected.json. Powers DESIGN.md §1
// claims 1 (token usage) and 2 (bug catch rate).
//
// The package is invoked via `pakka-core bench` (see cmd/pakka-core/main.go).
//
// Notes on the "raw" arm:
//
//	The current implementation runs raw claude with `--bare`, which skips
//	hook execution, plugin sync, attribution, auto-memory, keychain reads,
//	and CLAUDE.md auto-discovery. That is the closest available approximation
//	to "claude with no pakka." There is no narrower CLAUDE_DISABLE_PLUGINS
//	env var in current Claude Code; if/when one ships, swap the flag for it
//	to also exercise hooks/skills minus pakka. Documented limitation.
package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Entry mirrors one corpus.json record. `Expected` is unmarshalled lazily
// because seeded entries use a string path while real-pr entries inline an
// object.
type Entry struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Language     string          `json:"language"`
	BugClass     string          `json:"bug_class,omitempty"`
	Diff         string          `json:"diff"`
	Prompt       string          `json:"prompt"`
	Expected     json.RawMessage `json:"expected"`
	Repo         string          `json:"repo,omitempty"`
	PR           int             `json:"pr,omitempty"`
	BaselineToks int             `json:"baseline_tokens,omitempty"`

	// Resolved at load time.
	DiffPath     string   `json:"-"`
	PromptPath   string   `json:"-"`
	DiffBody     string   `json:"-"`
	PromptBody   string   `json:"-"`
	ExpectedBody Expected `json:"-"`
}

// Expected is the parsed expected outcome for an entry.
//
// For seeded-bug entries: BugClass + File + LineApprox describe the planted
// bug. For clean entries: Kind == "none" and ExpectedFindings == 0. For
// real-pr entries: ShouldBlock is the only meaningful field.
type Expected struct {
	Kind             string `json:"kind,omitempty"`
	BugClass         string `json:"bug_class,omitempty"`
	File             string `json:"file,omitempty"`
	LineApprox       int    `json:"line_approx,omitempty"`
	ExpectedFindings int    `json:"expected_findings,omitempty"`
	ShouldBlock      *bool  `json:"should_block,omitempty"`
}

// Finding is one structured finding extracted from the model's response.
//
// We only care about a small subset of fields for hit detection. Everything
// else is preserved as raw JSON inside the entry result for later inspection.
type Finding struct {
	Kind        string `json:"kind,omitempty"`
	Severity    string `json:"severity,omitempty"`
	File        string `json:"file,omitempty"`
	LineApprox  int    `json:"line_approx,omitempty"`
	Line        int    `json:"line,omitempty"`
	BugClass    string `json:"bug_class,omitempty"`
	Description string `json:"description,omitempty"`
}

// EntryResult is the per-arm outcome for one corpus entry.
type EntryResult struct {
	Tokens    int    `json:"tokens"`
	LatencyMs int64  `json:"latency_ms"`
	Hit       bool   `json:"hit"`
	FPCount   int    `json:"fp_count"`
	Findings  int    `json:"findings"`
	Error     string `json:"error,omitempty"`
}

// PerEntry pairs the entry id/kind with its raw and pakka results.
type PerEntry struct {
	ID    string       `json:"id"`
	Kind  string       `json:"kind"`
	Raw   *EntryResult `json:"raw,omitempty"`
	Pakka *EntryResult `json:"pakka,omitempty"`
}

// ArmSummary aggregates results across all entries for one arm.
type ArmSummary struct {
	TokensTotal    int `json:"tokens_total"`
	TokensAvg      int `json:"tokens_avg"`
	SeededHitCount int `json:"seeded_hit_count"`
	SeededTotal    int `json:"seeded_total"`
	FPCountClean   int `json:"fp_count_clean"`
	CleanTotal     int `json:"clean_total"`
}

// Output is the top-level results document.
type Output struct {
	TS         string                 `json:"ts"`
	Commit     string                 `json:"commit"`
	CorpusPath string                 `json:"corpus_path"`
	EntryCount int                    `json:"entry_count"`
	Modes      []string               `json:"modes"`
	PerEntry   []PerEntry             `json:"per_entry"`
	Summary    map[string]*ArmSummary `json:"summary"`
}

// Options controls a benchmark run.
type Options struct {
	CorpusPath string
	OutPath    string
	Limit      int
	Mode       string // "both" | "raw" | "pakka"
	ClaudeBin  string
	Timeout    time.Duration
	Verbose    bool
	Stderr     io.Writer
}

// runner abstracts the `claude -p` invocation so tests can swap a fake
// implementation. Tokens is the sum of input + cache_creation + cache_read +
// output tokens reported in the JSON response's `usage` field.
type runner interface {
	Run(ctx context.Context, mode string, input string) (response string, tokens int, err error)
}

// Run executes the full benchmark per opts and writes the results JSON.
//
// Returns an error only for fatal startup conditions (missing corpus, missing
// claude binary, unwritable out path). Per-entry failures are recorded inline
// in the output and do not abort the run.
func Run(opts Options) error {
	return run(opts, nil)
}

// run is the internal entrypoint that accepts an injected runner for tests.
// If r is nil, a real shell-based runner is constructed from opts.
func run(opts Options, r runner) error {
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Mode == "" {
		opts.Mode = "both"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 180 * time.Second
	}
	if opts.ClaudeBin == "" {
		opts.ClaudeBin = "claude"
	}

	modes, err := resolveModes(opts.Mode)
	if err != nil {
		return err
	}

	if r == nil {
		// Verify claude binary exists before any entry runs.
		if _, err := exec.LookPath(opts.ClaudeBin); err != nil {
			return fmt.Errorf("claude binary %q not on PATH: %w", opts.ClaudeBin, err)
		}
		r = &shellRunner{bin: opts.ClaudeBin, timeout: opts.Timeout}
	}

	entries, corpusDir, err := LoadCorpus(opts.CorpusPath)
	if err != nil {
		return fmt.Errorf("load corpus %s: %w", opts.CorpusPath, err)
	}

	// Apply --limit BEFORE reading prompt/diff/expected bodies so that a
	// malformed entry past the limit doesn't break a partial run.
	if opts.Limit > 0 && opts.Limit < len(entries) {
		entries = entries[:opts.Limit]
	}

	for i := range entries {
		if err := loadEntryBodies(&entries[i], corpusDir); err != nil {
			return fmt.Errorf("load corpus %s: %w", opts.CorpusPath, err)
		}
	}

	out := &Output{
		TS:         time.Now().UTC().Format(time.RFC3339),
		Commit:     gitCommit(corpusDir),
		CorpusPath: opts.CorpusPath,
		EntryCount: len(entries),
		Modes:      modes,
		Summary:    map[string]*ArmSummary{},
	}
	for _, m := range modes {
		out.Summary[m] = &ArmSummary{}
	}

	for i, e := range entries {
		if opts.Verbose {
			fmt.Fprintf(opts.Stderr, "[%d/%d] %s (%s)\n", i+1, len(entries), e.ID, e.Kind)
		}
		input := BuildInput(e.PromptBody, e.DiffBody)
		expected := e.ExpectedBody

		pe := PerEntry{ID: e.ID, Kind: e.Kind}
		for _, m := range modes {
			res, findings := runOneWithFindings(r, m, input, opts.Timeout)
			classifyResult(res, findings, e, expected)
			switch m {
			case "raw":
				pe.Raw = res
			case "pakka":
				pe.Pakka = res
			}
			updateSummary(out.Summary[m], e, res)
		}
		out.PerEntry = append(out.PerEntry, pe)
	}

	for _, s := range out.Summary {
		if out.EntryCount > 0 {
			s.TokensAvg = s.TokensTotal / out.EntryCount
		}
	}

	if err := writeOutput(opts.OutPath, out); err != nil {
		return fmt.Errorf("write %s: %w", opts.OutPath, err)
	}

	if opts.Verbose {
		fmt.Fprintf(opts.Stderr, "wrote %s\n", opts.OutPath)
	}
	return nil
}

// resolveModes maps the --mode flag value to a slice of arm names.
func resolveModes(m string) ([]string, error) {
	switch m {
	case "both":
		return []string{"raw", "pakka"}, nil
	case "raw":
		return []string{"raw"}, nil
	case "pakka":
		return []string{"pakka"}, nil
	default:
		return nil, fmt.Errorf("invalid --mode %q (expected both|raw|pakka)", m)
	}
}

// LoadCorpus reads corpus.json and parses entries. It does NOT read prompt,
// diff, or expected bodies — those are loaded per-entry by loadEntryBodies
// after --limit slicing so a malformed entry past the limit doesn't break a
// partial run. Returns the parsed entries and the corpus dir (used for git
// rev-parse and path resolution).
func LoadCorpus(path string) ([]Entry, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, "", fmt.Errorf("malformed corpus: %w", err)
	}
	return entries, filepath.Dir(abs), nil
}

// loadEntryBodies resolves and reads prompt, diff, and expected bodies for
// one entry, populating PromptPath/DiffPath/PromptBody/DiffBody/ExpectedBody.
//
// Path resolution is tolerant: each rel path is tried first against corpusDir
// (the directory holding corpus.json), then against corpusDir's parent (the
// repo root). The corpus has historically mixed both conventions — seeded
// entries use corpus-dir-relative paths ("seeds/01/..."), real-PR entries
// use repo-root-relative paths ("benchmarks/diffs/..."). Both are accepted.
func loadEntryBodies(e *Entry, corpusDir string) error {
	promptPath, err := resolveCorpusFile(e.Prompt, corpusDir)
	if err != nil {
		return fmt.Errorf("entry %s: read prompt: %w", e.ID, err)
	}
	e.PromptPath = promptPath
	pb, err := os.ReadFile(promptPath)
	if err != nil {
		return fmt.Errorf("entry %s: read prompt: %w", e.ID, err)
	}
	e.PromptBody = string(pb)

	diffPath, err := resolveCorpusFile(e.Diff, corpusDir)
	if err != nil {
		return fmt.Errorf("entry %s: read diff: %w", e.ID, err)
	}
	e.DiffPath = diffPath
	db, err := os.ReadFile(diffPath)
	if err != nil {
		return fmt.Errorf("entry %s: read diff: %w", e.ID, err)
	}
	e.DiffBody = string(db)

	exp, err := loadExpected(e.Expected, corpusDir)
	if err != nil {
		return fmt.Errorf("entry %s: expected: %w", e.ID, err)
	}
	e.ExpectedBody = exp
	return nil
}

// resolveCorpusFile tries rel against corpusDir first, then against
// corpusDir's parent (the repo root). The first existing path wins. If
// neither resolves, returns an error citing both attempted paths.
//
// Absolute paths are returned as-is (no fallback) when they exist.
func resolveCorpusFile(rel, corpusDir string) (string, error) {
	if filepath.IsAbs(rel) {
		if _, err := os.Stat(rel); err == nil {
			return rel, nil
		}
		return "", fmt.Errorf("path not found: %s", rel)
	}
	primary := filepath.Join(corpusDir, rel)
	if _, err := os.Stat(primary); err == nil {
		return primary, nil
	}
	fallback := filepath.Join(filepath.Dir(corpusDir), rel)
	if _, err := os.Stat(fallback); err == nil {
		return fallback, nil
	}
	return "", fmt.Errorf("path not found in either base: %s | %s", primary, fallback)
}

// loadExpected resolves a corpus entry's Expected raw JSON into a populated
// Expected struct. The raw JSON may be either:
//
//   - a string path (relative to corpusDir) pointing to an expected.json file
//   - an inline object (real-pr entries use this form)
//
// An empty Expected is returned for missing/malformed payloads — downstream
// hit detectors handle the zero case.
func loadExpected(raw json.RawMessage, corpusDir string) (Expected, error) {
	var exp Expected
	if len(raw) == 0 {
		return exp, nil
	}

	// Try string form first (the common case for seed/clean entries).
	var asPath string
	if err := json.Unmarshal(raw, &asPath); err == nil {
		full, err := resolveCorpusFile(asPath, corpusDir)
		if err != nil {
			return exp, err
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return exp, fmt.Errorf("read %s: %w", full, err)
		}
		// expected.json is a single JSON object, not JSONL.
		if err := json.Unmarshal(data, &exp); err != nil {
			return exp, fmt.Errorf("parse %s: %w", full, err)
		}
		return exp, nil
	}

	// Inline object form (real-pr entries).
	if err := json.Unmarshal(raw, &exp); err != nil {
		return exp, fmt.Errorf("inline expected: %w", err)
	}
	return exp, nil
}

// BuildInput composes the prompt + diff payload sent to `claude -p`.
func BuildInput(prompt, diff string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(prompt, "\n"))
	b.WriteString("\n\n```diff\n")
	b.WriteString(diff)
	if !strings.HasSuffix(diff, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n")
	return b.String()
}

// ExtractFindings scans response text line-by-line and returns every line
// that successfully unmarshals as a Finding object containing at least one
// recognized field.
//
// Free-form prose surrounding JSON lines is ignored; an empty response
// returns an empty slice with no error.
func ExtractFindings(response string) []Finding {
	var out []Finding
	for _, raw := range strings.Split(response, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || (line[0] != '{') {
			continue
		}
		var f Finding
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			continue
		}
		// Require at least one meaningful field. A bare `{}` or unrelated
		// JSON object on a line is not a finding.
		if f.Kind == "" && f.BugClass == "" && f.File == "" && f.Severity == "" && f.Description == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// HitSeeded returns true if any finding matches the expected seeded bug.
//
// Match rules (per task spec):
//  1. finding.bug_class == expected.BugClass, OR
//  2. finding.file == expected.File AND |finding.line - expected.LineApprox| <= 5
//
// File comparison uses the basename to tolerate path-prefix differences
// between what the model emits and what the expected file says.
func HitSeeded(findings []Finding, exp Expected) bool {
	for _, f := range findings {
		if exp.BugClass != "" && f.BugClass == exp.BugClass {
			return true
		}
		if exp.File != "" && sameFile(f.File, exp.File) {
			line := f.LineApprox
			if line == 0 {
				line = f.Line
			}
			if exp.LineApprox == 0 || absInt(line-exp.LineApprox) <= 5 {
				return true
			}
		}
	}
	return false
}

// HitClean returns true iff no findings were emitted (no false positives).
func HitClean(findings []Finding) bool {
	return len(findings) == 0
}

// classifyResult fills Hit and FPCount on res based on entry kind.
//
//	seeded-bug: Hit = HitSeeded; FPCount = 0.
//	clean:      Hit = HitClean;  FPCount = number of findings emitted.
//	real-pr:    log-only — Hit and FPCount remain zero.
//
// On a transport error (res.Error != ""), classification is skipped.
func classifyResult(res *EntryResult, findings []Finding, e Entry, exp Expected) {
	if res.Error != "" {
		return
	}
	switch e.Kind {
	case "seeded-bug":
		res.Hit = HitSeeded(findings, exp)
	case "clean":
		res.Hit = HitClean(findings)
		if !res.Hit {
			res.FPCount = len(findings)
		}
	}
}

// runOneWithFindings executes one arm for one entry and returns the populated
// result plus the parsed findings list. Used by run() so the caller can
// classify hits without re-running extraction.
func runOneWithFindings(r runner, mode, input string, timeout time.Duration) (*EntryResult, []Finding) {
	res := &EntryResult{}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, tokens, err := r.Run(ctx, mode, input)
	res.LatencyMs = time.Since(start).Milliseconds()
	res.Tokens = tokens
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	findings := ExtractFindings(resp)
	res.Findings = len(findings)
	return res, findings
}

// updateSummary increments arm-level counters from one per-entry result.
func updateSummary(s *ArmSummary, e Entry, res *EntryResult) {
	s.TokensTotal += res.Tokens
	switch e.Kind {
	case "seeded-bug":
		s.SeededTotal++
		if res.Hit {
			s.SeededHitCount++
		}
	case "clean":
		s.CleanTotal++
		s.FPCountClean += res.FPCount
	}
}

// writeOutput marshals out to JSON and writes to path, creating parent dirs.
func writeOutput(path string, out *Output) error {
	if path == "" {
		return errors.New("empty out path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// gitCommit returns the current HEAD SHA for repoDir, or "unknown" on error.
func gitCommit(repoDir string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// sameFile compares two file paths by basename, case-sensitive. The model
// often emits a path that omits a leading directory ("handlers/users.go"
// vs "users.go"); basename comparison forgives that.
func sameFile(a, b string) bool {
	return filepath.Base(a) == filepath.Base(b)
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// --- shellRunner: real claude -p invocation ---

type shellRunner struct {
	bin     string
	timeout time.Duration
}

// Run shells out to `claude -p --output-format=json`. For mode == "raw" we
// pass --bare to skip plugins/hooks/CLAUDE.md auto-discovery (closest
// available approximation to "no pakka"). For mode == "pakka" we inherit
// env/config so the user-installed pakka plugin is active.
func (s *shellRunner) Run(ctx context.Context, mode, input string) (string, int, error) {
	args := []string{"-p", "--output-format=json"}
	if mode == "raw" {
		args = append(args, "--bare")
	}

	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = os.Environ()

	out, err := cmd.Output()
	if err != nil {
		return "", 0, err
	}

	// Parse the wrapper JSON to extract result text + usage.
	var wrap struct {
		Result string `json:"result"`
		Usage  struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &wrap); err != nil {
		return string(out), 0, fmt.Errorf("parse claude json: %w", err)
	}

	tokens := wrap.Usage.InputTokens +
		wrap.Usage.CacheCreationInputTokens +
		wrap.Usage.CacheReadInputTokens +
		wrap.Usage.OutputTokens
	return wrap.Result, tokens, nil
}

