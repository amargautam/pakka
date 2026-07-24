// Package guard implements PreToolUse runtime checks for Read, Write, Edit, and Bash.
//
// Second-line defense after settings.json deny rules. Resolves symlinks
// (O_NOFOLLOW), detects live .env* files, introspects Bash commands for
// eval/curl-pipe-sh/directory-traversal. Bash heuristic blocks may be
// overridden per-repo via .pakka/guard-allowlist.json (see allowlist.go);
// secret-path and system-path denials never consult the allowlist.
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
	"time"

	"github.com/amargautam/pakka/internal/hookevent"
	"github.com/amargautam/pakka/internal/policy"
)

// Result of a guard check.
type Result struct {
	Allowed bool
	Reason  string
	// Pattern is the matched Bash heuristic category (eval, traversal, …).
	Pattern string
	// Allowlistable: the block may be overridden by the user; the caller
	// should emit an "ask" decision and write a pending override marker.
	Allowlistable bool
	// AllowlistedBy is set when the command was allowed via a recorded
	// per-repo override; audit note: guard_allowlisted=<pattern>.
	AllowlistedBy string
	// Warned: allowed because the pattern is demoted to warn in this repo.
	Warned bool
	// RepoRoot is the allowlist scope used for the decision.
	RepoRoot string
	// Shape is the normalized command, recorded on override.
	Shape string
	// AllowlistErr is non-empty when the allowlist file was malformed; the
	// block stands (fail closed) and the caller should audit-log the error.
	AllowlistErr string
	// PolicyLocked is set to the matched pattern when the committed
	// .pakka/policy.json locks the pattern's category: the learned allowlist is
	// ignored, the block stands, and no override is offered.
	PolicyLocked string
	// PolicyErr is non-empty when the committed policy file was malformed or
	// too-new; the block stands (fail closed) and the caller should audit-log it.
	PolicyErr string
}

// Run evaluates the hook event against guard rules with default thresholds.
//
// Purpose: Block reads of sensitive files and dangerous Bash commands at runtime.
// Errors: Never errors on policy — returns Result. Panics are bugs.
func Run(event *hookevent.Event) *Result {
	return RunWithConfig(event, DefaultConfig())
}

// RunWithConfig evaluates the hook event using the given allowlist thresholds.
func RunWithConfig(event *hookevent.Event, cfg Config) *Result {
	switch event.ToolName {
	case "Read":
		return checkRead(event)
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		return checkWrite(event)
	case "Bash":
		return checkBash(event, cfg)
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
	// The guard allowlist is system-managed: writes go through the override
	// flow, never through direct edits. Reads stay allowed (transparency).
	path, resolved := normalizePath(input.FilePath, event.CWD)
	for _, p := range []string{input.FilePath, path, resolved} {
		if isGuardAllowlistPath(p) {
			return &Result{Allowed: false, Reason: "blocked: pakka guard allowlist is system-managed (overrides are recorded automatically)"}
		}
	}
	return denyCheck(path, resolved)
}

// isGuardAllowlistPath reports whether p points at a .pakka/guard-allowlist.json.
func isGuardAllowlistPath(p string) bool {
	s := filepath.ToSlash(filepath.Clean(p))
	return s == ".pakka/"+allowlistFile || strings.HasSuffix(s, "/.pakka/"+allowlistFile)
}

// normalizePath expands ~, absolutizes against cwd, cleans, and resolves
// symlinks. Returns (lexical path, symlink-resolved path).
func normalizePath(raw, cwd string) (string, string) {
	home, _ := os.UserHomeDir()
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
	return path, resolved
}

func checkPath(raw, cwd string) *Result {
	path, resolved := normalizePath(raw, cwd)
	return denyCheck(path, resolved)
}

// denyCheck runs the secret-path deny rules on an already-normalized
// (lexical, symlink-resolved) path pair.
func denyCheck(path, resolved string) *Result {
	home, _ := os.UserHomeDir()
	// Canonicalize home to handle /var → /private/var on macOS
	if home != "" {
		if h, err := filepath.EvalSymlinks(home); err == nil {
			home = h
		}
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

// bashCheck is one Bash heuristic: pattern name, regex, block reason, and
// whether a user override may allowlist it per-repo.
type bashCheck struct {
	pattern       string
	re            *regexp.Regexp
	reason        string
	allowlistable bool
}

// bashChecks in evaluation order. Reasons are stable strings surfaced to the
// model; pattern names key the allowlist.
var bashChecks = []bashCheck{
	{"eval", evalRe, "blocked: eval usage", true},
	{"shell-c-eval", bashCEvalRe, "blocked: eval in shell -c argument", true},
	{"pipe-shell", pipeShellRe, "blocked: pipe to shell", true},
	{"download-exec", downloadExecRe, "blocked: download then execute", true},
	{"traversal", traversalRe, "blocked: directory traversal", true},
	{"system-path", absoluteDenyRe, "blocked: system path access", false},
}

// bashCheckFor returns the heuristic for a pattern name, or nil.
func bashCheckFor(pattern string) *bashCheck {
	for i := range bashChecks {
		if bashChecks[i].pattern == pattern {
			return &bashChecks[i]
		}
	}
	return nil
}

// checkAllowlistTamper default-denies any Bash command that mentions the
// guard allowlist file unless it is a single, plainly read-only command (no
// pipes, chains, or redirects). Enumerating write primitives is a losing
// game (dd, python -c, truncate, ln -sf, …), so mention = blocked unless
// provably read-only.
func checkAllowlistTamper(cmd string) *Result {
	if !strings.Contains(cmd, allowlistFile) {
		return nil
	}
	if allowlistReadOnlyRe.MatchString(cmd) {
		return nil
	}
	return &Result{Allowed: false, Reason: "blocked: pakka guard allowlist is system-managed (overrides are recorded automatically)", Pattern: "allowlist-tamper"}
}

func checkBash(event *hookevent.Event, cfg Config) *Result {
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(event.ToolInput, &input); err != nil {
		return &Result{Allowed: true}
	}
	cmd := input.Command

	if r := checkAllowlistTamper(cmd); r != nil {
		return r
	}

	var hit *bashCheck
	for i := range bashChecks {
		if bashChecks[i].re.MatchString(cmd) {
			hit = &bashChecks[i]
			break
		}
	}
	if hit == nil {
		return &Result{Allowed: true}
	}
	if !hit.allowlistable {
		return &Result{Allowed: false, Reason: hit.reason, Pattern: hit.pattern}
	}

	shape := Shape(cmd)
	root := RepoRoot(event.CWD)
	blocked := &Result{
		Allowed:       false,
		Reason:        hit.reason,
		Pattern:       hit.pattern,
		Allowlistable: root != "",
		RepoRoot:      root,
		Shape:         shape,
	}
	if root == "" {
		return blocked // no repo scope — plain block, no override offer
	}

	// Policy floor: a category the committed policy locks can never be
	// overridden by the learned per-repo allowlist — the block stands and no
	// override is offered. A malformed/too-new policy fails CLOSED the same way.
	// Absent policy → zero Policy → no-op, preserving pre-policy behavior.
	pol, perr := policy.Load(root)
	if perr != nil {
		blocked.Allowlistable = false
		blocked.PolicyErr = perr.Error()
		return blocked
	}
	if pol.IsCategoryLocked(hit.pattern) {
		blocked.Allowlistable = false
		blocked.PolicyLocked = hit.pattern
		return blocked
	}

	// In auto-approving permission modes the "ask" decision would be waved
	// through without a human — never offer the override path there.
	if event.PermissionMode == "bypassPermissions" || event.PermissionMode == "dontAsk" {
		blocked.Allowlistable = false
	}

	verdict, errStr := consultAllowlist(root, hit.pattern, shape, cfg, time.Now())
	if errStr != "" {
		// Malformed allowlist: fail closed — hard block, surface for audit.
		blocked.Allowlistable = false
		blocked.AllowlistErr = errStr
		return blocked
	}
	if verdict == verdictNone {
		return blocked
	}

	// An allowlist hit never weakens non-allowlistable categories: re-check
	// them before allowing (e.g. an allowlisted eval shape that also touches
	// /etc/passwd stays blocked).
	for i := range bashChecks {
		if !bashChecks[i].allowlistable && bashChecks[i].re.MatchString(cmd) {
			return &Result{Allowed: false, Reason: bashChecks[i].reason, Pattern: bashChecks[i].pattern}
		}
	}

	if verdict == verdictWarn {
		return &Result{Allowed: true, Warned: true, Pattern: hit.pattern, Reason: hit.reason, RepoRoot: root, Shape: shape}
	}
	return &Result{Allowed: true, AllowlistedBy: hit.pattern, Pattern: hit.pattern, RepoRoot: root, Shape: shape}
}

var (
	evalRe = regexp.MustCompile(`(?:^|[;&|]\s*|\$\(\s*)eval\b`)
	// bashCEvalRe detects eval inside a -c quoted shell argument.
	// Covers: bash -c "eval ...", sh -c 'eval ...', zsh/dash/fish/ksh variants.
	// [^;|&]* prevents matching across control operators between shell name and -c.
	bashCEvalRe = regexp.MustCompile(`(?i)\b(?:bash|sh|zsh|dash|fish|ksh)\s[^;|&]*-c\s+['"][^'"]*\beval\b`)
	// pipeShellRe detects fetcher piped directly to a shell interpreter.
	pipeShellRe = regexp.MustCompile(`(?i)\b(curl|wget)\b.*\|\s*(sh|bash|zsh|dash|fish|ksh|ash|csh)\b`)
	// downloadExecRe detects two-step download-then-execute:
	// curl -o /tmp/x <url> && bash /tmp/x  (or sh, zsh, etc.)
	downloadExecRe = regexp.MustCompile(`(?i)\b(curl|wget)\b.*-[oO]\s*\S+.*&&.*\b(sh|bash|zsh|dash|fish|ksh|ash|csh)\b`)
	traversalRe    = regexp.MustCompile(`(?:\.\./){2,}`)
	// absoluteDenyRe blocks access to high-value system paths that are never
	// legitimate in a dev Bash workflow. Intentionally narrow — only paths
	// where the security risk clearly outweighs false-positive cost.
	absoluteDenyRe = regexp.MustCompile(`(?:^|[\s'"])(/(?:etc/(?:passwd|shadow|sudoers|master\.passwd)|root\b|proc/self/(?:environ|mem|maps)|sys/kernel|private/etc/(?:passwd|shadow|sudoers|master\.passwd)))`)
	// allowlistReadOnlyRe matches the only commands allowed to mention the
	// guard allowlist: a single read-only tool with plain arguments — no
	// pipes, command chains, redirects, or substitutions. Everything else
	// that mentions the file is treated as tampering (default deny).
	allowlistReadOnlyRe = regexp.MustCompile(`^\s*(?:cat|jq|less|more|head|tail|grep|wc|stat|file|diff|ls|md5|md5sum|shasum|sha256sum)\b[^|;&><$` + "`" + `]*$`)
)
