package specfind

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// ClaudeLLM implements LLMCaller by shelling out to `claude -p`.
// Uses the same flag set as internal/compress/semantic/claude_cli.go:
//   -p --output-format text --permission-mode default --allowedTools ""
type ClaudeLLM struct {
	Path string // default "claude"
}

// NewClaudeLLM returns a ClaudeLLM with safe defaults.
func NewClaudeLLM() *ClaudeLLM {
	return &ClaudeLLM{Path: "claude"}
}

// Call pipes prompt to claude -p stdin and returns trimmed stdout.
func (c *ClaudeLLM) Call(prompt string) (string, error) {
	path := c.Path
	if path == "" {
		path = "claude"
	}
	args := []string{
		"-p",
		"--output-format", "text",
		"--permission-mode", "default",
		"--allowedTools", "",
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("specfind: claude exit: %w (stderr: %s)",
			err, snippet(stderr.Bytes(), 512))
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("specfind: claude empty response")
	}
	return out, nil
}

// snippet returns up to n bytes of b as a string.
func snippet(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}
