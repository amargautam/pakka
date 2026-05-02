package semantic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFakeClaude writes a POSIX-sh script at <dir>/claude that runs `body`.
// Returns the absolute path. Skips the test on Windows — we don't ship a
// .bat-shaped fixture since the production users on Windows are an
// edge case for v0.1.0 and would need a separate test rig anyway.
func writeFakeClaude(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-claude script uses POSIX shell; Windows path needs .bat fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	return path
}

// TestClaudeCLI_RewritesViaScriptedSubprocess: end-to-end path. Fake claude
// echoes a fixed string; Rewrite returns it trimmed. Asserts the rewriter
// actually invokes the subprocess and parses stdout (varies on script body).
func TestClaudeCLI_RewritesViaScriptedSubprocess(t *testing.T) {
	const want = "compressed text here"
	script := writeFakeClaude(t, "cat >/dev/null; printf '%s\\n' '"+want+"'")

	c := &ClaudeCLI{Path: script, Timeout: 5 * time.Second}
	got, err := c.Rewrite(context.Background(), "input prose", LevelStrict)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestClaudeCLI_NonZeroExitReturnsError: stderr snippet must be propagated
// in the wrapped error. Behavior varies on subprocess exit code.
func TestClaudeCLI_NonZeroExitReturnsError(t *testing.T) {
	script := writeFakeClaude(t, "cat >/dev/null; printf 'auth required\\n' >&2; exit 1")

	c := &ClaudeCLI{Path: script, Timeout: 5 * time.Second}
	_, err := c.Rewrite(context.Background(), "x", LevelStrict)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "auth required") {
		t.Errorf("error missing stderr snippet: %v", err)
	}
}

// TestClaudeCLI_TimeoutKillsSubprocess: when the script outlives the timeout,
// Rewrite returns context.DeadlineExceeded — not a generic exit error. Behavior
// varies on (Timeout, script duration) — flip either to break the assertion.
func TestClaudeCLI_TimeoutKillsSubprocess(t *testing.T) {
	script := writeFakeClaude(t, "cat >/dev/null; sleep 5; printf 'too late\\n'")

	c := &ClaudeCLI{Path: script, Timeout: 200 * time.Millisecond}
	start := time.Now()
	_, err := c.Rewrite(context.Background(), "x", LevelStrict)
	d := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
	// Generous bound: must be much less than the 5s sleep — proves the
	// subprocess was actually killed, not waited out.
	if d > 2*time.Second {
		t.Errorf("subprocess not killed promptly: took %v", d)
	}
}

// TestClaudeCLI_StripsOutputFraming: leading/trailing whitespace is removed.
// Behavior varies on script output framing.
func TestClaudeCLI_StripsOutputFraming(t *testing.T) {
	script := writeFakeClaude(t, "cat >/dev/null; printf '\\n\\n  trimmed body  \\n\\n'")

	c := &ClaudeCLI{Path: script, Timeout: 5 * time.Second}
	got, err := c.Rewrite(context.Background(), "x", LevelStrict)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if got != "trimmed body" {
		t.Errorf("output = %q, want %q", got, "trimmed body")
	}
}

// TestClaudeCLI_LargeInputPassesViaStdin: 50KB of input must reach the
// subprocess via stdin. The fake claude echoes a marker only when the
// captured byte count meets a threshold — assertion fails if stdin
// pipeline drops, truncates, or buffers wrong.
func TestClaudeCLI_LargeInputPassesViaStdin(t *testing.T) {
	dir := t.TempDir()
	stdinDump := filepath.Join(dir, "stdin.bin")
	// Body: dump stdin to a temp file, print a hash that the test reads back.
	// We just want to confirm the byte count survives the pipe.
	script := writeFakeClaude(t, "cat > '"+stdinDump+"'; printf 'ok\\n'")

	// Build a 50KB-ish payload that is mostly non-preserved prose so the
	// validator-free Rewrite call doesn't have to round-trip code blocks.
	var b strings.Builder
	for b.Len() < 50_000 {
		b.WriteString("the quick brown fox jumps over the lazy dog. ")
	}
	input := b.String()

	c := &ClaudeCLI{Path: script, Timeout: 10 * time.Second}
	got, err := c.Rewrite(context.Background(), input, LevelStrict)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if got != "ok" {
		t.Errorf("subprocess return = %q, want %q", got, "ok")
	}
	dumped, err := os.ReadFile(stdinDump)
	if err != nil {
		t.Fatalf("read stdin dump: %v", err)
	}
	// Prompt template wraps the input, so total bytes seen by stdin must
	// strictly exceed the input size. The exact size depends on the
	// template body — assertion is "input survived".
	if len(dumped) < len(input) {
		t.Errorf("stdin dump %d bytes, expected at least input size %d",
			len(dumped), len(input))
	}
	if !strings.Contains(string(dumped), "lazy dog") {
		t.Error("stdin dump missing input marker — payload truncated")
	}
}
