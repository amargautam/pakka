// Package guard implements PreToolUse runtime checks for Read, Write, Edit, and Bash.
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
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		return checkWrite(event)
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

func checkWrite(event *hookevent.Event) *Result {
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
	// Secret key file extensions and common credential filenames.
	switch {
	case strings.HasSuffix(base, ".pem"),
		strings.HasSuffix(base, ".p12"),
		strings.HasSuffix(base, ".pfx"),
		strings.HasSuffix(base, ".key"):
		return "blocked: private key file"
	case strings.HasPrefix(base, "id_rsa"),
		strings.HasPrefix(base, "id_ed25519"),
		strings.HasPrefix(base, "id_ecdsa"),
		strings.HasPrefix(base, "id_dsa"):
		return "blocked: SSH private key"
	case base == "credentials.json":
		return "blocked: credentials file"
	case strings.HasPrefix(base, "service-account") && strings.HasSuffix(base, ".json"):
		return "blocked: service account key"
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
	// Package manager tokens.
	if path == filepath.Join(home, ".npmrc") {
		return "blocked: npm credentials"
	}
	if path == filepath.Join(home, ".pypirc") {
		return "blocked: PyPI credentials"
	}
	// Shell history (may contain typed tokens/passwords).
	if path == filepath.Join(home, ".bash_history") {
		return "blocked: shell history"
	}
	if path == filepath.Join(home, ".zsh_history") {
		return "blocked: shell history"
	}
	if path == filepath.Join(home, ".zsh_sessions") || isUnder(path, filepath.Join(home, ".zsh_sessions")) {
		return "blocked: shell history"
	}
	// GitHub CLI token.
	if path == filepath.Join(home, ".config", "gh", "hosts.yml") {
		return "blocked: GitHub CLI credentials"
	}
	// Kubernetes cluster credentials.
	if path == filepath.Join(home, ".kube", "config") {
		return "blocked: Kubernetes credentials"
	}
	// Docker registry credentials.
	if path == filepath.Join(home, ".docker", "config.json") {
		return "blocked: Docker credentials"
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
	if bashCEvalRe.MatchString(cmd) {
		return &Result{Allowed: false, Reason: "blocked: eval in shell -c argument"}
	}
	if pipeShellRe.MatchString(cmd) {
		return &Result{Allowed: false, Reason: "blocked: pipe to shell"}
	}
	if downloadExecRe.MatchString(cmd) {
		return &Result{Allowed: false, Reason: "blocked: download then execute"}
	}
	if traversalRe.MatchString(cmd) {
		return &Result{Allowed: false, Reason: "blocked: directory traversal"}
	}
	if absoluteDenyRe.MatchString(cmd) {
		return &Result{Allowed: false, Reason: "blocked: system path access"}
	}
	return &Result{Allowed: true}
}

var (
	evalRe      = regexp.MustCompile(`(?:^|[;&|]\s*|\$\(\s*)eval\b`)
	// bashCEvalRe detects eval inside a -c quoted shell argument.
	// Covers: bash -c "eval ...", sh -c 'eval ...', zsh/dash/fish/ksh variants.
	// [^;|&]* prevents matching across control operators between shell name and -c.
	bashCEvalRe = regexp.MustCompile(`(?i)\b(?:bash|sh|zsh|dash|fish|ksh)\s[^;|&]*-c\s+['"][^'"]*\beval\b`)
	// pipeShellRe detects fetcher piped directly to a shell interpreter.
	pipeShellRe = regexp.MustCompile(`(?i)\b(curl|wget)\b.*\|\s*(sh|bash|zsh|dash|fish|ksh|ash|csh)\b`)
	// downloadExecRe detects two-step download-then-execute:
	// curl -o /tmp/x <url> && bash /tmp/x  (or sh, zsh, etc.)
	downloadExecRe = regexp.MustCompile(`(?i)\b(curl|wget)\b.*-[oO]\s*\S+.*&&.*\b(sh|bash|zsh|dash|fish|ksh|ash|csh)\b`)
	traversalRe = regexp.MustCompile(`(?:\.\./){2,}`)
	// absoluteDenyRe blocks access to high-value system paths that are never
	// legitimate in a dev Bash workflow. Intentionally narrow — only paths
	// where the security risk clearly outweighs false-positive cost.
	absoluteDenyRe = regexp.MustCompile(`(?:^|[\s'"])(/(?:etc/(?:passwd|shadow|sudoers|master\.passwd)|root\b|proc/self/(?:environ|mem|maps)|sys/kernel|private/etc/(?:passwd|shadow|sudoers|master\.passwd)))`)
)
