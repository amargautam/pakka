// ClaudeCLI rewriter — invokes the `claude` CLI (Claude Code) in non-interactive
// `-p` print mode and reuses the user's existing OAuth/keychain auth, so users
// don't need to set ANTHROPIC_API_KEY just to use semantic compression.
//
// Why subprocess instead of HTTP:
//   - Pakka is a Claude Code plugin; every user already has `claude` on PATH.
//   - The HTTP path requires ANTHROPIC_API_KEY, which most Claude Code users
//     never set (they auth via OAuth/keychain). That's wrong UX.
//   - `claude -p --bare` would skip auth entirely (Pass 5b finding); plain
//     `claude -p` reuses OAuth and works zero-config.
//   - We accept the per-call init overhead. Compression already runs in a
//     forked background process (Pass 4.3), so latency is not in the
//     SessionStart hot path.
//
// Auth resolution lives in cmd/pakka-core/orchestrator.go: ClaudeCLI is the
// primary path; AnthropicClient is the fallback when `claude` is missing
// from PATH but ANTHROPIC_API_KEY is set; otherwise the orchestrator no-ops.
package semantic

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/claudecli"
)

// killProcessGroup is set by claude_cli_unix.go on POSIX builds and
// claude_cli_windows.go on Windows. It returns a Cancel function that
// kills the entire process group rooted at cmd.Process so that descendant
// processes (e.g. a `sh -c "sleep 5; ..."` child) don't outlive the
// timeout. On Windows the implementation falls back to the default
// process kill — Setpgid is POSIX-specific.
var configureProcessGroup func(cmd *exec.Cmd)

// ClaudeCLI implements Rewriter by shelling out to `claude -p`.
//
// Path defaults to "claude" (resolved via PATH at exec time). Tests override
// it with a path to a fake POSIX-shell script that echoes a canned response.
//
// Timeout caps a single rewrite call. Default 180s. The validator/cherry-pick
// retry loop in runner.go can call Rewrite up to 3 times, so the orchestrator
// effective ceiling is ~3× this value.
//
// Model is optional. Empty string means "let Claude Code pick its default
// model" — we don't force `claude-haiku-...` via this path because the user's
// local Claude Code config may already have a model preference.
type ClaudeCLI struct {
	Path    string
	Timeout time.Duration
	Model   string
}

// NewClaudeCLI returns a ClaudeCLI with safe defaults.
//
// Purpose: One-line constructor used by orchestrator wiring.
// Errors: None. Existence of `claude` on PATH is the caller's responsibility
// (orchestrator.go uses exec.LookPath before constructing).
func NewClaudeCLI() *ClaudeCLI {
	return &ClaudeCLI{
		Path:    "claude",
		Timeout: 180 * time.Second,
	}
}

// Rewrite renders the per-level prompt template and pipes it to `claude -p`
// on stdin. Stdout is captured, trimmed, and returned. On non-zero exit a
// wrapped error is returned containing a truncated stderr snippet.
//
// Purpose: Production semantic-rewrite path used when the `claude` CLI is on
// PATH (the common case for any Claude Code user).
// Errors:
//   - Template render error (unlikely; templates are static).
//   - exec start error (claude binary missing or not executable).
//   - context cancellation / deadline (subprocess killed; returns ctx.Err()).
//   - non-zero exit (wrapped, with stderr snippet ≤ 512 bytes).
//   - empty stdout after trim (treated as "empty response" error).
func (c *ClaudeCLI) Rewrite(ctx context.Context, input string, level Level) (string, error) {
	prompt, err := renderPrompt(level, input)
	if err != nil {
		return "", err
	}
	return c.run(ctx, prompt)
}

// RewriteFix renders the cherry-pick fix template and pipes it the same way.
// Implements the FixRewriter optional interface so the runner uses the
// violation-aware retry prompt.
func (c *ClaudeCLI) RewriteFix(ctx context.Context, input string, level Level, violations []Violation) (string, error) {
	prompt, err := renderFixPrompt(level, input, violations)
	if err != nil {
		return "", err
	}
	return c.run(ctx, prompt)
}

// run executes `claude -p ...` with the prompt on stdin and returns the
// trimmed stdout.
//
// Flag choice rationale:
//
//   - `-p` / `--print`: documented non-interactive print mode. Required.
//   - `--output-format text`: emit plain text (default, but stated explicitly
//     so future Claude Code default changes don't break parse).
//   - `--permission-mode default` + `--allowedTools ""`: this subprocess only
//     needs to emit text. A prompt-injection in the compressed file content
//     could otherwise trick the rewriter into invoking tools — earlier
//     versions used `bypassPermissions`, which would let any such injection
//     run unrestricted. `default` keeps tool gating on, and the empty
//     allowlist denies every tool, so the worst case is a denied request,
//     not arbitrary execution.
//
// We deliberately do NOT pass `--bare`. Per Pass 5b: `--bare` strips OAuth
// and forces ANTHROPIC_API_KEY, which defeats the whole point of this path.
func (c *ClaudeCLI) run(parentCtx context.Context, prompt string) (string, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	path := c.Path
	if path == "" {
		path = "claude"
	}

	args := claudecli.BuildArgs(c.Model)

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if configureProcessGroup != nil {
		configureProcessGroup(cmd)
	}

	err := cmd.Run()

	// Honor context errors over generic exit errors — a deadline-killed
	// subprocess shows up as an exit error from cmd.Run, but the caller
	// wants ctx.Err() so retries / cancellation propagate cleanly.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}

	if err != nil {
		return "", fmt.Errorf("semantic: claude exit: %w (stderr: %s)",
			err, snippet(stderr.Bytes(), 512))
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("semantic: claude empty response")
	}
	return out, nil
}
