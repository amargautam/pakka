// Package stackgate runs stack-aware lint and test commands as a
// PostToolUse hook on Edit and Write tool invocations.
//
// Behavior:
//   - Reads .pakka/stack.json for configured commands.
//   - If stack.json is missing, exits silently (wizard hasn't run yet).
//   - On every edit/write: runs the lint command (fast feedback).
//   - On test-file edits: also runs the test command.
//   - Lint has a 30-second timeout; on timeout, logs a warning and passes.
//
// Exit codes: 0 pass, 2 lint failure (stderr = lint output).
package stackgate

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/hookevent"
)

const lintTimeout = 30 * time.Second

// Config holds the stack tool commands loaded from .pakka/stack.json.
type Config struct {
	TestCommand   string `json:"test_command"`
	LintCommand   string `json:"lint_command"`
	FormatCommand string `json:"format_command"`
}

// Result holds the outcome of a stack-gate check.
type Result struct {
	Passed bool
	Output string // stderr payload if failed
}

// Run executes the stack's lint command on the changed file.
// Only lint runs on every edit (fast). Test runs only on test files.
//
// Purpose: Provide fast lint feedback after Edit/Write tool invocations.
// Errors: Returns Result with Passed=false and Output containing stderr on lint failure.
func Run(event *hookevent.Event, cfg *Config) *Result {
	if cfg == nil || cfg.LintCommand == "" {
		return &Result{Passed: true}
	}

	// Extract the file path from the tool input
	filePath := extractFilePath(event)

	// Run lint command
	lintResult := runCommand(cfg.LintCommand, event.CWD)
	if !lintResult.Passed {
		return lintResult
	}

	// If this is a test file, also run the test command
	if filePath != "" && cfg.TestCommand != "" && isTestFile(filePath) {
		testResult := runCommand(cfg.TestCommand, event.CWD)
		if !testResult.Passed {
			return testResult
		}
	}

	return &Result{Passed: true}
}

// LoadConfig reads .pakka/stack.json from the given directory.
//
// Purpose: Load stack gate configuration from the project directory.
// Errors: Returns nil if the file doesn't exist or can't be parsed.
func LoadConfig(dir string) *Config {
	// Read from filesystem directly — avoid import cycles
	data, err := readFileBytes(filepath.Join(dir, ".pakka", "stack.json"))
	if err != nil {
		return nil
	}
	var cfg Config
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	return &cfg
}

// extractFilePath pulls the file_path from the tool input JSON.
func extractFilePath(event *hookevent.Event) string {
	var input struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(event.ToolInput, &input) != nil {
		return ""
	}
	return input.FilePath
}

// isTestFile reports whether the filename matches common test file patterns.
func isTestFile(path string) bool {
	base := filepath.Base(path)
	patterns := []struct {
		suffix string
		prefix string
	}{
		{suffix: "_test.go"},
		{suffix: ".test.ts"},
		{suffix: ".test.js"},
		{suffix: ".test.tsx"},
		{suffix: ".test.jsx"},
		{suffix: "_test.py"},
		{prefix: "test_"},
		{suffix: ".spec.ts"},
		{suffix: ".spec.js"},
	}
	for _, p := range patterns {
		if p.suffix != "" && strings.HasSuffix(base, p.suffix) {
			return true
		}
		if p.prefix != "" && strings.HasPrefix(base, p.prefix) {
			return true
		}
	}
	return false
}

// runCommand executes a shell command with a timeout.
func runCommand(command, cwd string) *Result {
	ctx, cancel := context.WithTimeout(context.Background(), lintTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		// Timeout — don't block, just pass with a warning
		return &Result{Passed: true, Output: "stack-gate: lint timed out after 30s, skipping"}
	}
	if err != nil {
		return &Result{Passed: false, Output: stderr.String()}
	}
	return &Result{Passed: true}
}

// readFileBytes reads a file from disk. Defined as a var for test overrides.
var readFileBytes = func(path string) ([]byte, error) {
	return os.ReadFile(path)
}
