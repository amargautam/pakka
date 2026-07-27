package calibrate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AgentRef records which reviewer prompt file was fed to the harness and its
// content hash, so an artifact is reproducible against an exact prompt version.
// body holds the frontmatter-stripped instruction text sent to the model; it
// is unexported so it never lands in the artifact JSON.
type AgentRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	body   string `json:"-"`
}

// SeedResult is the per-seed record written to the artifact.
type SeedResult struct {
	Seed          string    `json:"seed"`
	Kind          string    `json:"kind"`
	Expected      Expected  `json:"expected"`
	Findings      []Finding `json:"findings"`
	Recalled      bool      `json:"recalled"`
	FalsePositive bool      `json:"falsePositive"`
	Timeout       bool      `json:"timeout,omitempty"`
	Error         string    `json:"error,omitempty"`
	// ParsedNothing is true when at least one reviewer returned a non-empty
	// response yet zero findings parsed out of the whole seed — the signature
	// of a format/parse failure rather than a genuine clean pass.
	ParsedNothing bool `json:"parsedNothing,omitempty"`
	// Scored is true when this seed produced a reviewer verdict and fed the
	// aggregate rates. False for timeout/error seeds (recorded but excluded).
	Scored bool `json:"scored"`
}

// Artifact is the top-level calibration document written to
// benchmarks/results/calibration-<date>.json and read back by the report.
type Artifact struct {
	Date      string       `json:"date"`
	Commit    string       `json:"commit,omitempty"`
	Model     string       `json:"model,omitempty"`
	Threshold int          `json:"threshold"`
	Agents    []AgentRef   `json:"agents"`
	Seeds     []SeedResult `json:"seeds"`
	Aggregate Aggregate    `json:"aggregate"`
}

// Options controls a calibration run.
type Options struct {
	// SeedsDir holds the seed fixtures (one subdir per seed, each with
	// seed.patch + expected.json). Default benchmarks/seeds.
	SeedsDir string
	// AgentFiles are the reviewer prompt markdown files fed to each seed.
	// Default agents/{reviewer,security,performance,architect}.md.
	AgentFiles []string
	// OutDir is where calibration-<date>.json is written. Default
	// benchmarks/results.
	OutDir string
	// RepoRoot is the pakka repo root, used to resolve default paths and to
	// stamp the artifact's commit. Default ".".
	RepoRoot string
	// Threshold is the confidence floor for a finding to count. Default 80
	// (the gate's default confidenceThreshold).
	Threshold int
	// SeedTimeout bounds one seed's total model time; on exceed the seed is
	// marked "timeout" and the run continues. Default 300s.
	SeedTimeout time.Duration
	// ClaudeBin is the claude CLI. Default "claude".
	ClaudeBin string
	// Date stamps the artifact and filename (YYYY-MM-DD). Empty = today (UTC).
	Date string
	// Stdout/Stderr for progress. Default os.Stdout/os.Stderr.
	Stdout io.Writer
	Stderr io.Writer

	// runner is an injected model invoker for tests. nil = real claude -p.
	runner agentRunner
}

// agentRunner abstracts one `claude -p` reviewer invocation so tests can inject
// a fake without a model. It returns the agent's raw response text plus the
// model name the CLI reported (empty when unknown).
type agentRunner interface {
	Run(ctx context.Context, workdir, systemPrompt, userPrompt string) (response, model string, err error)
}

// defaultAgentFiles is the reviewer prompt set the live gate uses, in a stable
// order.
var defaultAgentFiles = []string{
	"agents/reviewer.md",
	"agents/security.md",
	"agents/performance.md",
	"agents/architect.md",
}

// SkipError signals a named, non-fatal skip: the harness intentionally wrote
// nothing (e.g. claude CLI absent). The CLI prints it and exits 0.
type SkipError struct{ Reason string }

func (e *SkipError) Error() string { return e.Reason }

// Run executes the calibration: for each seed it materializes a temp repo,
// stages the seed patch, invokes every reviewer agent via `claude -p`, parses
// findings, scores them, and writes the artifact. It returns *SkipError when
// the claude CLI is absent (caller exits 0, nothing written).
//
// Auth: OAuth session via the claude CLI on PATH only. The environment API-key
// variable is never read; an API key is not a fallback (see the grep guard
// test that forbids that variable anywhere in this package's source).
func Run(opts Options) error {
	fillDefaults(&opts)

	// Auth guard: the claude CLI must be on PATH. Absent → named skip, exit 0,
	// write nothing. We never consult the environment API-key variable — an API
	// key is not a substitute for the OAuth session.
	if opts.runner == nil {
		if _, err := exec.LookPath(opts.ClaudeBin); err != nil {
			return &SkipError{Reason: fmt.Sprintf(
				"calibrate: claude CLI %q not found on PATH — skipping calibration (no scores written)", opts.ClaudeBin)}
		}
		opts.runner = &shellRunner{bin: opts.ClaudeBin}
	}

	agents, err := loadAgents(opts.RepoRoot, opts.AgentFiles)
	if err != nil {
		return err
	}

	seedDirs, err := discoverSeeds(opts.SeedsDir)
	if err != nil {
		return err
	}
	if len(seedDirs) == 0 {
		return fmt.Errorf("calibrate: no seeds found in %s", opts.SeedsDir)
	}

	art := &Artifact{
		Date:      opts.Date,
		Commit:    gitCommit(opts.RepoRoot),
		Threshold: opts.Threshold,
		Agents:    agents,
	}

	var scores []SeedScore
	var counts RunCounts
	parsedNothing := 0
	for i, dir := range seedDirs {
		name := filepath.Base(dir)
		fmt.Fprintf(opts.Stdout, "[%d/%d] %s\n", i+1, len(seedDirs), name)

		res := runSeed(opts, agents, dir)
		if art.Model == "" && res.model != "" {
			art.Model = res.model
		}

		// A seed that timed out or errored produced no reliable reviewer
		// verdict: record it with its status but EXCLUDE it from the rates so
		// an infrastructure failure never deflates recall. Scored seeds only
		// feed Aggregated.
		switch {
		case res.result.Timeout:
			counts.Timeout++
		case res.result.Error != "":
			counts.Error++
		default:
			res.result.Scored = true
			counts.Scored++
			if res.result.ParsedNothing {
				parsedNothing++
			}
			score := Score(res.result.Findings, res.result.Expected, opts.Threshold)
			res.result.Recalled = score.Recalled
			res.result.FalsePositive = score.FalsePositive
			scores = append(scores, score)
		}
		art.Seeds = append(art.Seeds, res.result)
	}

	art.Aggregate = Aggregated(scores)
	art.Aggregate.Model = art.Model
	art.Aggregate.Counts = counts
	art.Aggregate.Degraded = DegradedByParseFailure(parsedNothing, counts.Scored)

	outPath := filepath.Join(opts.OutDir, "calibration-"+opts.Date+".json")
	if err := writeArtifact(outPath, art); err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "wrote %s (recall=%.2f precision=%.2f fpRate=%.2f n=%d)\n",
		outPath, art.Aggregate.Recall, art.Aggregate.Precision, art.Aggregate.FPRate, art.Aggregate.N)
	return nil
}

func fillDefaults(o *Options) {
	if o.RepoRoot == "" {
		o.RepoRoot = "."
	}
	if o.SeedsDir == "" {
		o.SeedsDir = filepath.Join(o.RepoRoot, "benchmarks", "seeds")
	}
	if o.OutDir == "" {
		o.OutDir = filepath.Join(o.RepoRoot, "benchmarks", "results")
	}
	if len(o.AgentFiles) == 0 {
		o.AgentFiles = defaultAgentFiles
	}
	if o.Threshold == 0 {
		o.Threshold = 80
	}
	if o.SeedTimeout == 0 {
		o.SeedTimeout = 300 * time.Second
	}
	if o.ClaudeBin == "" {
		o.ClaudeBin = "claude"
	}
	if o.Date == "" {
		o.Date = time.Now().UTC().Format("2006-01-02")
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
}

// seedRun bundles a seed's artifact record with the model name observed.
type seedRun struct {
	result SeedResult
	model  string
}

// runSeed materializes one seed into a temp repo, invokes every agent under a
// per-seed deadline, and collects findings. On deadline it marks the seed
// timeout and returns whatever was collected so far; the caller continues.
func runSeed(opts Options, agents []AgentRef, seedDir string) seedRun {
	name := filepath.Base(seedDir)
	exp, kind, err := loadExpected(seedDir)
	sr := seedRun{result: SeedResult{Seed: name, Kind: kind, Expected: exp}}
	if err != nil {
		sr.result.Error = err.Error()
		return sr
	}

	repo, cleanup, err := materializeSeed(seedDir)
	if err != nil {
		sr.result.Error = err.Error()
		return sr
	}
	defer cleanup()

	userPrompt := seedUserPrompt(seedDir)

	ctx, cancel := context.WithTimeout(context.Background(), opts.SeedTimeout)
	defer cancel()

	sawNonEmpty := false
	for _, ag := range agents {
		sys := ag.body
		resp, model, err := opts.runner.Run(ctx, repo, sys, userPrompt)
		if model != "" && sr.model == "" {
			sr.model = model
		}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				sr.result.Timeout = true
				break
			}
			// A single agent transport error is non-fatal: record it and move
			// on to the remaining agents for this seed.
			if sr.result.Error == "" {
				sr.result.Error = err.Error()
			}
			continue
		}
		if strings.TrimSpace(resp) != "" {
			sawNonEmpty = true
		}
		sr.result.Findings = append(sr.result.Findings, ParseFindings(resp)...)
	}
	// A seed where a reviewer answered (non-empty text) but nothing parsed is
	// suspect: it is indistinguishable from a genuine clean pass and, in bulk,
	// signals a systemic format/parse failure. Flag it so the aggregate can
	// mark the run degraded rather than report a bogus rate of 0.
	sr.result.ParsedNothing = sawNonEmpty && len(sr.result.Findings) == 0
	return sr
}

// ParseFindings scans response text line-by-line, returning every line that
// parses as a Finding object with at least one meaningful field. Prose and
// junk lines are tolerated and skipped.
func ParseFindings(response string) []Finding {
	var out []Finding
	for _, raw := range strings.Split(response, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] != '{' {
			continue
		}
		var f Finding
		if json.Unmarshal([]byte(line), &f) != nil {
			continue
		}
		if f.Kind == "" && f.BugClass == "" && f.File == "" && f.Severity == "" && f.Rationale == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// loadAgents reads each agent prompt file, records its SHA, and strips the
// YAML frontmatter to get the instruction body fed to the model.
func loadAgents(repoRoot string, files []string) ([]AgentRef, error) {
	var refs []AgentRef
	for _, f := range files {
		path := f
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoRoot, f)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("calibrate: read agent %s: %w", f, err)
		}
		sum := sha256.Sum256(data)
		refs = append(refs, AgentRef{
			File:   f,
			SHA256: fmt.Sprintf("%x", sum),
			body:   stripFrontmatter(string(data)),
		})
	}
	return refs, nil
}

// stripFrontmatter removes a leading YAML frontmatter block (--- ... ---) so
// only the instruction body is sent as the system prompt.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---") {
		return s
	}
	rest := s[3:]
	// find the closing delimiter at a line start
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return s
	}
	after := rest[idx+4:] // past "\n---"
	if nl := strings.IndexByte(after, '\n'); nl >= 0 {
		after = after[nl+1:]
	}
	return strings.TrimLeft(after, "\n")
}

// discoverSeeds returns the seed subdirectories that contain expected.json,
// sorted by name for deterministic ordering.
func discoverSeeds(seedsDir string) ([]string, error) {
	entries, err := os.ReadDir(seedsDir)
	if err != nil {
		return nil, fmt.Errorf("calibrate: read seeds %s: %w", seedsDir, err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(seedsDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "expected.json")); err == nil {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// loadExpected reads a seed's expected.json into an Expected, returning the
// artifact "kind" string (seeded-bug | clean | performance) inferred from it.
func loadExpected(seedDir string) (Expected, string, error) {
	data, err := os.ReadFile(filepath.Join(seedDir, "expected.json"))
	if err != nil {
		return Expected{}, "", fmt.Errorf("read expected.json: %w", err)
	}
	var exp Expected
	if err := json.Unmarshal(data, &exp); err != nil {
		return Expected{}, "", fmt.Errorf("parse expected.json: %w", err)
	}
	return exp, seedKind(exp), nil
}

// seedKind maps an Expected to the corpus kind label recorded in the artifact.
func seedKind(exp Expected) string {
	if exp.IsClean() {
		return "clean"
	}
	if exp.Kind == "performance" {
		return "performance"
	}
	return "seeded-bug"
}

// seedUserPrompt returns the seed's prompt.md if present, else a default review
// instruction. The staged diff is available in the temp repo for the agent to
// read via `git diff --cached`.
func seedUserPrompt(seedDir string) string {
	if data, err := os.ReadFile(filepath.Join(seedDir, "prompt.md")); err == nil {
		return string(data)
	}
	return "Review the staged diff (`git diff --cached`) for correctness, security, and performance issues. Report findings as JSON lines."
}

// writeArtifact marshals the artifact to JSON at path, creating parent dirs.
func writeArtifact(path string, art *Artifact) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(art, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// gitCommit returns HEAD for repoRoot, or "" on error.
func gitCommit(repoRoot string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
