// Package guard implements PreToolUse runtime checks for Read and Bash.
//
// Second-line defense after settings.json deny rules. Resolves symlinks
// (O_NOFOLLOW), detects live .env* files, introspects Bash commands for
// eval/curl-pipe-sh/directory-traversal.
//
// Exit codes: 0 allow, 2 block (stderr shown to model), 1 internal error.
// Must stay under 5ms p95 cold.
package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/amargautam/pakka/internal/hookevent"
)

// Result of a guard check.
type Result struct {
	Allowed bool
	Reason  string
}

// Run evaluates the hook event against guard rules.
//
// Purpose: Block reads of sensitive files and dangerous Bash commands at runtime.
// Errors: Never errors on policy — returns Result. Panics are bugs.
func Run(event *hookevent.Event) *Result {
	switch event.ToolName {
	case "Read":
		return checkRead(event)
	case "Bash":
		return checkBash(event)
	default:
		return &Result{Allowed: true}
	}
}

// --- Read checks ---

func checkRead(event *hookevent.Event) *Result {
	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(event.ToolInput, &input); err != nil {
		return &Result{Allowed: true}
	}
	return checkPath(input.FilePath, event.CWD)
}

func checkPath(raw, cwd string) *Result {
	home, _ := os.UserHomeDir()
	// Canonicalize home to handle /var → /private/var on macOS
	if home != "" {
		if h, err := filepath.EvalSymlinks(home); err == nil {
			home = h
		}
	}

	path := raw
	if strings.HasPrefix(path, "~/") && home != "" {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) && cwd != "" {
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)

	// O_NOFOLLOW: resolve symlinks to check the real target
	resolved := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		resolved = r
	}

	for _, p := range []string{path, resolved} {
		if reason := isDeniedPath(p, home); reason != "" {
			return &Result{Allowed: false, Reason: reason}
		}
	}
	return &Result{Allowed: true}
}

func isDeniedPath(path, home string) string {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".env") {
		return "blocked: .env file"
	}
	if home == "" {
		return ""
	}
	if isUnder(path, filepath.Join(home, ".ssh")) {
		return "blocked: SSH key directory"
	}
	if isUnder(path, filepath.Join(home, ".aws")) {
		return "blocked: AWS credentials"
	}
	if isUnder(path, filepath.Join(home, ".gnupg")) {
		return "blocked: GPG keyring"
	}
	if path == filepath.Join(home, ".netrc") {
		return "blocked: .netrc file"
	}
	return ""
}

func isUnder(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// --- Bash checks ---

func checkBash(event *hookevent.Event) *Result {
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(event.ToolInput, &input); err != nil {
		return &Result{Allowed: true}
	}
	cmd := input.Command

	if evalRe.MatchString(cmd) {
		return &Result{Allowed: false, Reason: "blocked: eval usage"}
	}
	if pipeShellRe.MatchString(cmd) {
		return &Result{Allowed: false, Reason: "blocked: pipe to shell"}
	}
	if traversalRe.MatchString(cmd) {
		return &Result{Allowed: false, Reason: "blocked: directory traversal"}
	}
	return &Result{Allowed: true}
}

var (
	evalRe      = regexp.MustCompile(`(?:^|[;&|]\s*|\$\(\s*)eval\b`)
	pipeShellRe = regexp.MustCompile(`(?i)\b(curl|wget)\b.*\|\s*(sh|bash|zsh)\b`)
	traversalRe = regexp.MustCompile(`(?:\.\./){2,}`)
)
