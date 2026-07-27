package calibrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// materializeSeed builds a throwaway git repo, applies the seed patch, and
// stages it so the reviewed change appears as `git diff --cached`. It returns
// the repo path and a cleanup func. The temp repo is created OUTSIDE the pakka
// tree (os.MkdirTemp default) so a run never dirties the source repo.
func materializeSeed(seedDir string) (repo string, cleanup func(), err error) {
	repo, err = os.MkdirTemp("", "pakka-calibrate-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { os.RemoveAll(repo) }

	// Minimal, isolated repo: no global config, no signing, deterministic id.
	steps := [][]string{
		{"init", "-q"},
		{"config", "user.email", "calibrate@pakka.dev"},
		{"config", "user.name", "pakka-calibrate"},
		{"config", "commit.gpgsign", "false"},
	}
	for _, s := range steps {
		if out, e := runGit(repo, s...); e != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("git %s: %v: %s", strings.Join(s, " "), e, out)
		}
	}

	// Absolutize the patch path BEFORE the cwd switch: git apply runs with
	// cmd.Dir = the temp repo, so a relative seed path (the production case —
	// calibrate is invoked with --repo-root=. so seedDir is
	// benchmarks/seeds/<seed>) would otherwise be resolved against the temp dir
	// and fail with "can't find/open" (exit 128).
	patch := filepath.Join(seedDir, "seed.patch")
	patch, err = filepath.Abs(patch)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("resolve patch path: %w", err)
	}
	// Apply into the working tree, then stage everything. The seed patches add
	// new files from /dev/null, so a plain apply + add -A stages the change.
	if out, e := runGit(repo, "apply", "--whitespace=nowarn", patch); e != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("git apply %s: %v: %s", patch, e, out)
	}
	if out, e := runGit(repo, "add", "-A"); e != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("git add: %v: %s", e, out)
	}
	return repo, cleanup, nil
}

// runGit runs a git command in dir and returns combined output.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// shellRunner invokes one reviewer agent via `claude -p --output-format json`
// with the inherited OAuth session. The agent's instruction body is passed as
// an appended system prompt; the seed prompt is the user turn on stdin. cwd is
// the materialized seed repo so the agent's `git diff --cached` / Read work.
//
// No API key is read, set, or required — argv never includes --bare and the
// environment is inherited unchanged (never augmented with a key).
type shellRunner struct {
	bin string
}

// claudeResult is the subset of the claude -p JSON wrapper we consume.
type claudeResult struct {
	Result string `json:"result"`
	Model  string `json:"model"`
}

func (s *shellRunner) Run(ctx context.Context, workdir, systemPrompt, userPrompt string) (string, string, error) {
	args := []string{
		"-p",
		"--output-format", "json",
		"--permission-mode", "default",
		"--append-system-prompt", systemPrompt,
	}
	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(userPrompt)
	// Inherit the environment unchanged: the OAuth session travels in it. We do
	// NOT add or read any API-key variable — the OAuth session is the only auth.
	cmd.Env = os.Environ()

	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	var wrap claudeResult
	if e := json.Unmarshal(out, &wrap); e != nil {
		// Tolerate a non-JSON wrapper by returning the raw text; findings
		// parsing still salvages any JSON lines. Model stays unknown.
		return string(out), "", nil
	}
	return wrap.Result, wrap.Model, nil
}
