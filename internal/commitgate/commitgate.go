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
	SessionID           string // nonce added to trailer to prevent pre-planting forgery
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
// sessionID is embedded as a short nonce to prevent pre-planting forgery.
// Errors: None.
func BaselineTrailer(version, sessionID string) string {
	if sid := shortSID(sessionID); sid != "" {
		return fmt.Sprintf("%s v%s (sid:%s)", trailerKeyA, version, sid)
	}
	return fmt.Sprintf("%s v%s", trailerKeyA, version)
}

// StrongTrailer returns the strong (review-passed) trailer value (Trailer A).
//
// Purpose: Trailer for commits that passed the review gate.
// sessionID is embedded as a short nonce to prevent pre-planting forgery.
// Errors: None.
func StrongTrailer(version, sessionID string) string {
	if sid := shortSID(sessionID); sid != "" {
		return fmt.Sprintf("%s v%s (gate: passed, sid:%s)", trailerKeyA, version, sid)
	}
	return fmt.Sprintf("%s v%s (gate: passed)", trailerKeyA, version)
}

// shortSID sanitizes a session ID to [A-Za-z0-9_-] and returns the first 8
// characters of the result, or the full sanitized ID if 8 chars or fewer.
// Returns "" for empty input or input with no safe characters.
func shortSID(sid string) string {
	var b strings.Builder
	for _, r := range sid {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	clean := b.String()
	if len(clean) > 8 {
		return clean[:8]
	}
	return clean
}

// CoAuthorTrailer returns the Co-authored-by trailer value (Trailer B).
//
// Purpose: GitHub contributor attribution for pakka-bot.
// Errors: None.
func CoAuthorTrailer() string {
	return fmt.Sprintf("Co-authored-by: pakka <%s>", coAuthorPakkaEmail)
}

// IsGitCommit reports whether cmd is a recognizable git commit command.
//
// Purpose: Detect commit shapes the gate can rewrite. Three are accepted:
//  1. bare:    `git commit ...`
//  2. -C form: `git -C <path> commit ...`
//  3. cd-wrap: `cd <path> && git commit ...` (single segment; no trailing chain)
//
// Chained commits (`git commit && git push`, `git commit; foo`) are rejected:
// trailer injection inside a chain has too many failure modes. See
// extractCommitSegment for details.
//
// Errors: None.
func IsGitCommit(cmd string) bool {
	_, ok, _ := extractCommitSegment(cmd)
	return ok
}

// extractCommitSegment locates the `git commit` portion of cmd in one of the
// three recognized shapes. Returns:
//   - segment: the substring starting at `git commit` (or `git -C ... commit`),
//     extending to end of cmd. For wrapped shapes, this is the suffix after
//     the wrapper.
//   - ok: true if the shape is recognized.
//   - prefixLen: byte offset of segment within cmd (so InjectTrailer can
//     splice trailers into the right place).
//
// Rejection rules:
//   - control operators (`&&`, `;`, `|`, `>`) anywhere in the commit segment
//     itself (after the segment's start) — but NOT inside quoted args.
//   - more than one `git commit` occurrence in cmd.
//   - cd chains with more than one segment (e.g. `cd /a && cd /b && git commit ...`).
//   - any prefix other than `cd <arg> && ` before a wrapped `git commit`.
func extractCommitSegment(cmd string) (segment string, ok bool, prefixLen int) {
	// Multiple `git commit` occurrences → reject. We use a string scan that
	// requires word boundaries on both sides; this catches `git commit && git commit`
	// while leaving `git commit-graph` alone.
	if countCommitOccurrences(cmd) > 1 {
		return "", false, 0
	}

	trimmed := strings.TrimLeft(cmd, " \t\n")
	leading := len(cmd) - len(trimmed)

	// Wrapped: `cd <arg> && git commit ...`.
	if strings.HasPrefix(trimmed, "cd ") || strings.HasPrefix(trimmed, "cd\t") {
		// Parse `cd <arg>` then require ` && ` (with surrounding whitespace).
		i := 2 // past "cd"
		for i < len(trimmed) && isWS(trimmed[i]) {
			i++
		}
		if i >= len(trimmed) {
			return "", false, 0
		}
		// Read one shell-quoted-or-bare argument as the cd target.
		_, next := readQuotedArg(trimmed, i)
		i = next
		// Skip whitespace, expect literal `&&`.
		for i < len(trimmed) && isWS(trimmed[i]) {
			i++
		}
		if i+1 >= len(trimmed) || trimmed[i] != '&' || trimmed[i+1] != '&' {
			return "", false, 0
		}
		i += 2
		for i < len(trimmed) && isWS(trimmed[i]) {
			i++
		}
		// What remains must be a bare/`-C` git commit segment with no further chain.
		seg, ok2 := parseGitCommitHead(trimmed[i:])
		if !ok2 {
			return "", false, 0
		}
		// Reject any control operator in the segment outside quotes.
		if hasUnquotedControlOp(seg) {
			return "", false, 0
		}
		return seg, true, leading + i
	}

	// Bare or -C form: `git ...`.
	if strings.HasPrefix(trimmed, "git ") || strings.HasPrefix(trimmed, "git\t") {
		seg, ok2 := parseGitCommitHead(trimmed)
		if !ok2 {
			return "", false, 0
		}
		if hasUnquotedControlOp(seg) {
			return "", false, 0
		}
		return seg, true, leading
	}

	return "", false, 0
}

// parseGitCommitHead checks whether s begins with `git commit` or
// `git -C <arg> commit`, and returns s unchanged on success. Any other shape
// (other flags between `git` and `commit`, commit-graph, etc.) is rejected.
func parseGitCommitHead(s string) (string, bool) {
	// Must start with "git" + WS.
	if !strings.HasPrefix(s, "git ") && !strings.HasPrefix(s, "git\t") {
		return "", false
	}
	i := 3
	for i < len(s) && isWS(s[i]) {
		i++
	}
	if i >= len(s) {
		return "", false
	}

	// `-C <arg>` is the only flag we accept between `git` and `commit`.
	if strings.HasPrefix(s[i:], "-C") && (i+2 < len(s) && (isWS(s[i+2]) || s[i+2] == '=')) {
		j := i + 2
		// `-C=path` not standard; advance past either ` ` or `=`.
		for j < len(s) && (isWS(s[j]) || s[j] == '=') {
			j++
		}
		if j >= len(s) {
			return "", false
		}
		_, next := readQuotedArg(s, j)
		j = next
		for j < len(s) && isWS(s[j]) {
			j++
		}
		// Now expect `commit` as the next word.
		if !strings.HasPrefix(s[j:], "commit") {
			return "", false
		}
		k := j + len("commit")
		if k != len(s) && !isWS(s[k]) {
			return "", false
		}
		return s, true
	}

	// Otherwise must be literal `commit` next.
	if !strings.HasPrefix(s[i:], "commit") {
		return "", false
	}
	k := i + len("commit")
	if k != len(s) && !isWS(s[k]) {
		return "", false
	}
	return s, true
}

// hasUnquotedControlOp reports whether s contains any of `&&`, `;`, `|`, `>`
// outside of a single- or double-quoted string. Used to reject chained shapes.
func hasUnquotedControlOp(s string) bool {
	i := 0
	n := len(s)
	for i < n {
		ch := s[i]
		switch ch {
		case '\'':
			i++
			for i < n && s[i] != '\'' {
				i++
			}
			if i < n {
				i++
			}
		case '"':
			i++
			for i < n {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if s[i] == '"' {
					i++
					break
				}
				i++
			}
		case '\\':
			// Escaped next char (outside quotes) — skip both.
			i += 2
		case '&':
			if i+1 < n && s[i+1] == '&' {
				return true
			}
			i++
		case ';', '|', '>':
			return true
		default:
			i++
		}
	}
	return false
}

// countCommitOccurrences counts non-overlapping `git commit` substrings in s
// that lie outside of single- or double-quoted strings. Word boundaries on
// the trailing side are enforced (so `git commit-graph` does not count).
//
// This is a conservative, syntactic count: it treats `git commit` and
// `git -C <path> commit` as separate occurrences. That is fine for our
// deduplication purposes — the -C form has only one literal `git commit`
// substring (the `commit` is preceded by `<path> ` not `git `), so it
// counts as 0 here. Callers should not rely on this returning 1 for valid
// shapes; they only check `> 1` to detect chained or duplicated commits.
func countCommitOccurrences(s string) int {
	count := 0
	i := 0
	n := len(s)
	for i < n {
		ch := s[i]
		switch ch {
		case '\'':
			i++
			for i < n && s[i] != '\'' {
				i++
			}
			if i < n {
				i++
			}
		case '"':
			i++
			for i < n {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if s[i] == '"' {
					i++
					break
				}
				i++
			}
		default:
			// `git commit` literal with leading word boundary and trailing
			// word boundary (end-of-string or whitespace).
			if (i == 0 || isWS(s[i-1]) || s[i-1] == '&' || s[i-1] == ';') &&
				strings.HasPrefix(s[i:], "git commit") {
				k := i + len("git commit")
				if k == n || isWS(s[k]) {
					count++
					i = k
					continue
				}
			}
			i++
		}
	}
	return count
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
	if lastBlank >= 0 {
		// Real trailer block (lines after the last blank line). Any
		// trailer-shaped line containing the marker counts as a hit.
		for _, line := range lines[lastBlank+1:] {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, marker) && looksLikeTrailer(trimmed) {
				return true
			}
		}
		return false
	}
	// No blank-line separator (e.g., single-line `-m` message that is
	// itself a trailer like `Reviewed-by-pakka: v0.1.0`). Scan every line
	// but require the line to START with the marker so a prose mention
	// like `docs: explain the Reviewed-by-pakka: trailer format` does not
	// accidentally suppress trailer injection.
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, marker) && looksLikeTrailer(trimmed) {
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
// in an intentional position: at the start or end of the subject line, or
// on its own line anywhere in the message body. Embedded mid-prose mentions
// (in the subject or body) do NOT count as a skip.
//
// Subject vs. body: the subject is the first line of the message. Prefix
// and suffix checks run against the trimmed subject so messages with a body
// (e.g. "feat: foo [skip pakka]\n\nbody...") still detect correctly. The
// standalone-line scan covers markers placed on their own line in the body.
//
// Purpose: Detect user opt-out per commit.
// Errors: None.
func HasSkipMarker(cmd string) bool {
	msg := parseGitCommitArgs(cmd).Message
	if !strings.Contains(msg, "[skip pakka]") {
		return false
	}

	// Subject = first line of the message, trimmed. Prefix/suffix checks
	// must target the subject only — running HasSuffix against the whole
	// message fails the moment the commit has a body.
	subject := msg
	if nl := strings.IndexByte(msg, '\n'); nl >= 0 {
		subject = msg[:nl]
	}
	subject = strings.TrimSpace(subject)
	if strings.HasPrefix(subject, "[skip pakka]") {
		return true
	}
	if strings.HasSuffix(subject, "[skip pakka]") {
		return true
	}

	// Standalone marker on its own line in the body (existing behavior).
	for _, line := range strings.Split(msg, "\n") {
		if strings.TrimSpace(line) == "[skip pakka]" {
			return true
		}
	}
	return false
}

// InjectTrailer appends --trailer to a git commit command.
//
// Purpose: Rewrite git commit to include the pakka trailer. Handles all three
// recognized shapes (bare `git commit`, `git -C <path> commit`, and the
// `cd <path> && git commit` wrapper). For wrapped shapes, the trailer is
// spliced inside the `git commit ...` portion so the cd context is preserved.
// Falls back to plain append for unrecognized shapes (defense-in-depth: callers
// gate on IsGitCommit, but appending a stray flag is harmless if reached).
//
// Security: The trailer value is shell-quoted before concatenation. Single-quote
// wrapping is content-agnostic — embedded quotes, $, backticks, backslashes, and
// newlines are neutralised. Callers may pass attacker-controlled trailer text
// without risk of shell injection in the resulting Bash command.
// Errors: None.
func InjectTrailer(cmd, trailer string) string {
	// For all three recognized shapes, the segment runs to end-of-line
	// (chains are rejected by extractCommitSegment), so a plain append works.
	// We still call extractCommitSegment to validate, but the splice point
	// for currently-supported shapes is always end-of-string.
	return cmd + ` --trailer ` + shellQuote(trailer)
}

// shellQuote wraps s in single quotes, escaping any embedded single quote
// via the standard '\'' sequence. Result is safe for direct interpolation
// into a Bash command line; the shell will see exactly s as one argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Evaluate determines the commit-gate action for a Bash command.
//
// Purpose: Pure decision logic for commit gating. No I/O, no side effects.
// Errors: Never errors; always returns a valid Decision.
func Evaluate(cmd string, cfg *Config, state *State) *Decision {
	// Not a git commit — pass through, unless the command still contains
	// "git commit" in a shape the gate cannot safely parse (e.g. compound
	// commands with ;, &&, |, >).  Those must be blocked, not waved through.
	if !IsGitCommit(cmd) {
		if strings.Contains(cmd, "git commit") {
			return &Decision{
				Allow:  false,
				Stderr: "pakka: unrecognized git commit shape — gate cannot verify safety; use a plain form or add [skip pakka] to bypass",
			}
		}
		return &Decision{Allow: true}
	}

	// Nothing to do: no trailers and no gate.
	if !cfg.Signature && !cfg.CoAuthor && !cfg.AutoGate {
		return &Decision{Allow: true}
	}

	// Per-commit skip → allow, no trailers, no gate.
	// Emit a stderr notice so skips are visible; audit note uses neutral tag
	// since the gate cannot distinguish human-provided vs model-inserted markers.
	if HasSkipMarker(cmd) {
		return &Decision{
			Allow:     true,
			AuditNote: "review_skipped=skip_marker",
			Stderr:    "pakka: [skip pakka] detected — gate, trailers, and audit bypassed for this commit",
		}
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
			d := maybeRewrite(BaselineTrailer(cfg.Version, cfg.SessionID))
			d.AuditNote = "review_skipped=oversize"
			return d
		}

		// Recent passing review — strong trailer.
		if state.HasRecentPass {
			return maybeRewrite(StrongTrailer(cfg.Version, cfg.SessionID))
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
	return maybeRewrite(BaselineTrailer(cfg.Version, cfg.SessionID))
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
