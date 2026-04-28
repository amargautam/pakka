// Package commitgate implements the PreToolUse commit-gate logic.
//
// Matches Bash tool calls starting with "git commit", determines whether
// to allow/block/rewrite the command based on review state and config.
// Pure decision logic — no I/O. Callers supply Config and State.
//
// Exit codes (applied by caller): 0 = allow, 2 = block.
package commitgate

import (
	"fmt"
	"strings"
)

const (
	trailerKeyA      = "Reviewed-by-pakka:"
	coAuthorPakkaEmail = "279024857+pakka-bot@users.noreply.github.com"
)

// Config holds commit-gate settings from pakka config.
type Config struct {
	AutoGate            bool
	Signature           bool
	CoAuthor            bool
	ConfidenceThreshold int
	MaxDiffBytes        int
	SkipPaths           []string
	Version             string
}

// DefaultConfig returns design-doc defaults.
//
// Purpose: Provide baseline config when settings.json is absent or partial.
// Errors: None.
func DefaultConfig() *Config {
	return &Config{
		AutoGate:            true,
		Signature:           true,
		CoAuthor:            true,
		ConfidenceThreshold: 80,
		MaxDiffBytes:        200000,
		Version:             "0.1.0",
	}
}

// State captures external state needed for gate decisions.
type State struct {
	HasRecentPass bool      // last-pass-ts within threshold (300s)
	DiffBytes     int       // byte size of git diff --cached
	ErrorFindings []Finding // severity=error findings above confidence threshold
}

// Finding represents a review finding above threshold.
type Finding struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Severity   string `json:"severity"`
	Confidence int    `json:"confidence"`
	Rationale  string `json:"rationale"`
	Fix        string `json:"fix"`
}

// GetFile and GetLine make Finding satisfy diffscope.Finding so the gate
// can drop findings whose (file, line) is not in the staged-diff scope.
// Kept here (not in diffscope) so commitgate has no dependency on diffscope.
func (f Finding) GetFile() string { return f.File }
func (f Finding) GetLine() int    { return f.Line }

// Decision is the outcome of Evaluate.
type Decision struct {
	Allow     bool   // true = exit 0, false = exit 2
	Command   string // rewritten command; empty = no rewrite
	Stderr    string // message for stderr on block
	AuditNote string // note for audit log (review_skipped=reason)
}

// BaselineTrailer returns the baseline trailer value (Trailer A).
//
// Purpose: Trailer for commits without a passing review gate.
// Errors: None.
func BaselineTrailer(version string) string {
	return fmt.Sprintf("%s v%s", trailerKeyA, version)
}

// StrongTrailer returns the strong (review-passed) trailer value (Trailer A).
//
// Purpose: Trailer for commits that passed the review gate.
// Errors: None.
func StrongTrailer(version string) string {
	return fmt.Sprintf("%s v%s (gate: passed)", trailerKeyA, version)
}

// CoAuthorTrailer returns the Co-authored-by trailer value (Trailer B).
//
// Purpose: GitHub contributor attribution for pakka-bot.
// Errors: None.
func CoAuthorTrailer() string {
	return fmt.Sprintf("Co-authored-by: pakka <%s>", coAuthorPakkaEmail)
}

// IsGitCommit reports whether cmd is a git commit command.
//
// Purpose: Detect git commit variants (with flags, --amend, editor mode).
// Errors: None.
func IsGitCommit(cmd string) bool {
	trimmed := strings.TrimLeft(cmd, " \t\n")
	if !strings.HasPrefix(trimmed, "git commit") {
		return false
	}
	rest := trimmed[len("git commit"):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n'
}

// gitCommitParts holds parsed components of a git commit command.
type gitCommitParts struct {
	Message  string   // content of -m / --message argument(s), heredoc-unwrapped
	Trailers []string // values of --trailer arguments
}

// parseGitCommitArgs extracts the -m message and --trailer values from a
// git commit command string. Handles double-quoted, single-quoted, and
// heredoc-wrapped ($(...)) message values.
func parseGitCommitArgs(cmd string) gitCommitParts {
	var parts gitCommitParts
	i := 0
	n := len(cmd)
	for i < n {
		for i < n && isWS(cmd[i]) {
			i++
		}
		if i >= n {
			break
		}
		rest := cmd[i:]

		// --trailer or --trailer=
		if strings.HasPrefix(rest, "--trailer") {
			j := i + 9
			if j >= n || isWS(cmd[j]) || cmd[j] == '=' {
				for j < n && (isWS(cmd[j]) || cmd[j] == '=') {
					j++
				}
				val, next := readQuotedArg(cmd, j)
				parts.Trailers = append(parts.Trailers, val)
				i = next
				continue
			}
		}

		// --message or --message=
		if strings.HasPrefix(rest, "--message") {
			j := i + 9
			if j >= n || isWS(cmd[j]) || cmd[j] == '=' {
				for j < n && (isWS(cmd[j]) || cmd[j] == '=') {
					j++
				}
				val, next := readQuotedArg(cmd, j)
				parts.Message = appendMsg(parts.Message, unwrapHeredoc(val))
				i = next
				continue
			}
		}

		// -m (short form; not --)
		if strings.HasPrefix(rest, "-m") && !strings.HasPrefix(rest, "--") {
			j := i + 2
			for j < n && isWS(cmd[j]) {
				j++
			}
			if j < n {
				val, next := readQuotedArg(cmd, j)
				parts.Message = appendMsg(parts.Message, unwrapHeredoc(val))
				i = next
				continue
			}
			i = j
			continue
		}

		// Skip unrecognised token.
		if cmd[i] == '"' || cmd[i] == '\'' {
			_, i = readQuotedArg(cmd, i)
		} else {
			for i < n && !isWS(cmd[i]) {
				i++
			}
		}
	}
	return parts
}

func isWS(b byte) bool { return b == ' ' || b == '\t' || b == '\n' }

// appendMsg concatenates multiple -m values the way git does (blank line between).
func appendMsg(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "\n\n" + addition
}

// readQuotedArg reads a double-quoted, single-quoted, or bare argument.
// Tracks $() nesting depth so heredocs inside command substitutions
// don't cause early termination of the outer double-quoted string.
func readQuotedArg(cmd string, i int) (string, int) {
	if i >= len(cmd) {
		return "", i
	}
	n := len(cmd)
	switch cmd[i] {
	case '"':
		i++
		var b strings.Builder
		depth := 0
		for i < n {
			ch := cmd[i]
			if ch == '\\' && i+1 < n {
				next := cmd[i+1]
				if depth == 0 && (next == '\\' || next == '"' || next == '$' || next == '`') {
					b.WriteByte(next)
					i += 2
					continue
				}
				b.WriteByte(ch)
				b.WriteByte(next)
				i += 2
				continue
			}
			if ch == '$' && i+1 < n && cmd[i+1] == '(' {
				depth++
				b.WriteString("$(")
				i += 2
				continue
			}
			if ch == ')' && depth > 0 {
				depth--
				b.WriteByte(ch)
				i++
				continue
			}
			if ch == '"' && depth == 0 {
				i++
				return b.String(), i
			}
			b.WriteByte(ch)
			i++
		}
		return b.String(), i
	case '\'':
		i++
		start := i
		for i < n && cmd[i] != '\'' {
			i++
		}
		val := cmd[start:i]
		if i < n {
			i++
		}
		return val, i
	default:
		start := i
		for i < n && !isWS(cmd[i]) {
			i++
		}
		return cmd[start:i], i
	}
}

// unwrapHeredoc extracts content from $(cat <<'DELIM'...DELIM) wrappers.
// Returns input unchanged if not a heredoc.
func unwrapHeredoc(val string) string {
	if !strings.HasPrefix(val, "$(") {
		return val
	}
	idx := strings.Index(val, "<<")
	if idx < 0 {
		return val
	}
	rest := val[idx+2:]
	rest = strings.TrimLeft(rest, "-")

	var delim string
	if len(rest) > 0 && (rest[0] == '\'' || rest[0] == '"') {
		q := rest[0]
		end := strings.IndexByte(rest[1:], q)
		if end < 0 {
			return val
		}
		delim = rest[1 : 1+end]
		rest = rest[2+end:]
	} else {
		end := strings.IndexAny(rest, " \t\n")
		if end < 0 {
			return val
		}
		delim = rest[:end]
		rest = rest[end:]
	}
	if delim == "" {
		return val
	}
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		return val
	}
	body := rest[nl+1:]
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == delim {
			return strings.Join(lines[:i], "\n")
		}
	}
	return val
}

// messageHasTrailer checks whether marker appears in the trailer section
// of a commit message (lines after the last blank line, in Key: Value format).
func messageHasTrailer(msg, marker string) bool {
	lines := strings.Split(msg, "\n")
	lastBlank := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lastBlank = i
		}
	}
	if lastBlank < 0 {
		return false
	}
	for _, line := range lines[lastBlank+1:] {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, marker) && looksLikeTrailer(trimmed) {
			return true
		}
	}
	return false
}

// looksLikeTrailer returns true if the line matches trailer format (Key: Value).
func looksLikeTrailer(line string) bool {
	idx := strings.Index(line, ": ")
	if idx <= 0 {
		return false
	}
	key := line[:idx]
	return !strings.Contains(key, " ")
}

// HasTrailerA reports whether cmd already contains the Reviewed-by-pakka trailer
// in a --trailer flag or in the message body trailer section.
//
// Purpose: Idempotency for Trailer A.
// Errors: None.
func HasTrailerA(cmd string) bool {
	parts := parseGitCommitArgs(cmd)
	for _, t := range parts.Trailers {
		if strings.Contains(t, trailerKeyA) {
			return true
		}
	}
	return messageHasTrailer(parts.Message, trailerKeyA)
}

// HasTrailerB reports whether cmd already contains the pakka-bot Co-authored-by
// trailer. Uses exact email match to avoid colliding with Claude's Co-Authored-By.
//
// Purpose: Idempotency for Trailer B.
// Errors: None.
func HasTrailerB(cmd string) bool {
	parts := parseGitCommitArgs(cmd)
	for _, t := range parts.Trailers {
		if strings.Contains(t, coAuthorPakkaEmail) {
			return true
		}
	}
	return messageHasTrailer(parts.Message, coAuthorPakkaEmail)
}

// HasSkipMarker reports whether the commit message contains [skip pakka]
// in an intentional position (start of message, end of message, or own line).
// Does NOT match [skip pakka] embedded in prose elsewhere in the command.
//
// Purpose: Detect user opt-out per commit.
// Errors: None.
func HasSkipMarker(cmd string) bool {
	msg := parseGitCommitArgs(cmd).Message
	if !strings.Contains(msg, "[skip pakka]") {
		return false
	}
	trimmed := strings.TrimSpace(msg)
	if strings.HasPrefix(trimmed, "[skip pakka]") {
		return true
	}
	if strings.HasSuffix(trimmed, "[skip pakka]") {
		return true
	}
	for _, line := range strings.Split(msg, "\n") {
		if strings.TrimSpace(line) == "[skip pakka]" {
			return true
		}
	}
	return false
}

// InjectTrailer appends --trailer to a git commit command.
//
// Purpose: Rewrite git commit to include the pakka trailer.
// Errors: None.
func InjectTrailer(cmd, trailer string) string {
	return cmd + ` --trailer "` + trailer + `"`
}

// Evaluate determines the commit-gate action for a Bash command.
//
// Purpose: Pure decision logic for commit gating. No I/O, no side effects.
// Errors: Never errors; always returns a valid Decision.
func Evaluate(cmd string, cfg *Config, state *State) *Decision {
	// Not a git commit — pass through.
	if !IsGitCommit(cmd) {
		return &Decision{Allow: true}
	}

	// Nothing to do: no trailers and no gate.
	if !cfg.Signature && !cfg.CoAuthor && !cfg.AutoGate {
		return &Decision{Allow: true}
	}

	// Per-commit skip → allow, no trailers, no gate.
	if HasSkipMarker(cmd) {
		return &Decision{Allow: true, AuditNote: "review_skipped=user_skip"}
	}

	// Per-trailer idempotency.
	needA := cfg.Signature && !HasTrailerA(cmd)
	needB := cfg.CoAuthor && !HasTrailerB(cmd)

	// rewrite injects the needed trailers into cmd.
	rewrite := func(trailerAValue string) string {
		result := cmd
		if needA {
			result = InjectTrailer(result, trailerAValue)
		}
		if needB {
			result = InjectTrailer(result, CoAuthorTrailer())
		}
		return result
	}

	// maybeRewrite returns an allow decision, with a rewritten command
	// only if at least one trailer was injected.
	maybeRewrite := func(trailerAValue string) *Decision {
		rewritten := rewrite(trailerAValue)
		d := &Decision{Allow: true}
		if rewritten != cmd {
			d.Command = rewritten
		}
		return d
	}

	// Gate runs whenever AutoGate is on.
	// Trailer injection respects Signature/CoAuthor independently.
	if cfg.AutoGate {
		// Oversize diff — skip gate.
		if cfg.MaxDiffBytes > 0 && state.DiffBytes > cfg.MaxDiffBytes {
			d := maybeRewrite(BaselineTrailer(cfg.Version))
			d.AuditNote = "review_skipped=oversize"
			return d
		}

		// Recent passing review — strong trailer.
		if state.HasRecentPass {
			return maybeRewrite(StrongTrailer(cfg.Version))
		}

		// No recent pass — block.
		if len(state.ErrorFindings) > 0 {
			return &Decision{Allow: false, Stderr: FormatFindings(state.ErrorFindings)}
		}
		return &Decision{
			Allow:  false,
			Stderr: "pakka: review gate active. No passing review found.\nRun /pakka:review on staged changes, or add [skip pakka] to bypass.",
		}
	}

	// No gate — baseline Trailer A (if needed) + Trailer B (if needed).
	return maybeRewrite(BaselineTrailer(cfg.Version))
}

// FormatFindings formats error findings for stderr output.
//
// Purpose: Readable stderr block so the model can see and fix issues.
// Errors: None.
func FormatFindings(findings []Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pakka: review gate blocked commit. %d error(s) found:\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "\n[%s] %s:%d — %s (%d%%)\n", f.Severity, f.File, f.Line, f.Rationale, f.Confidence)
		if f.Fix != "" {
			fmt.Fprintf(&b, "  fix: %s\n", f.Fix)
		}
	}
	return b.String()
}
