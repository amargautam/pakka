// AST-based command parser for commitgate.
//
// Purpose: when the fast-path string matcher (IsGitCommit) rejects a command
// because it contains chains, redirects, env prefixes, subshells, or repeats
// — but the underlying shape is still a real commit invocation — fall back
// to a real POSIX shell parser (mvdan.cc/sh/v3) and splice the trailer into
// each commit CallExpr while preserving surrounding constructs verbatim.
//
// Out of scope: bash -c "..." (treated as dynamic; blocked), eval, and any
// dynamic command name where the command head is itself a substitution.
package commitgate

import (
	"bytes"
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// astResult is the outcome of attempting to splice trailers via AST.
type astResult struct {
	// recognized = the parser successfully understood the command and located
	// at least one git commit CallExpr. When false, callers should block.
	recognized bool
	// rejectCause is a short, user-facing string naming why a recognized
	// command was nonetheless rejected (e.g. "eval", "dynamic command name").
	// Only meaningful when recognized = false.
	rejectCause string
	// rewritten is the command string after trailer injection on every
	// commit node. Only meaningful when recognized = true.
	rewritten string
	// addedA, addedB report which trailers were actually appended during the
	// rewrite (false if already present on every node).
	addedA bool
	addedB bool
}

// astInjectTrailers parses cmd with the POSIX shell parser, walks every
// CallExpr, and splices --trailer flags into each git commit invocation.
//
// Returns recognized=false on parse failure or on detecting an unsupported
// construct (eval, dynamic command name, bash -c with a shell string). The
// rejectCause names the specific blocker so the gate can surface it to the
// model in stderr.
func astInjectTrailers(cmd string, wantA bool, trailerA string, wantB bool, trailerB string) astResult {
	r := strings.NewReader(cmd)
	parser := syntax.NewParser(syntax.KeepComments(true))
	file, err := parser.Parse(r, "")
	if err != nil {
		return astResult{recognized: false, rejectCause: "shell parse error: " + err.Error()}
	}

	commitNodes, cause := findCommitCallExprs(file)
	if cause != "" {
		return astResult{recognized: false, rejectCause: cause}
	}
	if len(commitNodes) == 0 {
		// No commit invocation found — caller's fast-path missed for a
		// different reason. Treat as not-a-commit by signalling recognized=false
		// with empty cause so the caller can decide what to do.
		return astResult{recognized: false, rejectCause: ""}
	}

	addedA := false
	addedB := false
	for _, node := range commitNodes {
		if wantA && !callExprHasTrailerValue(node, trailerKeyA) {
			appendTrailerToCall(node, trailerA)
			addedA = true
		}
		if wantB && !callExprHasTrailerValue(node, coAuthorPakkaEmail) {
			appendTrailerToCall(node, trailerB)
			addedB = true
		}
	}

	var buf bytes.Buffer
	printer := syntax.NewPrinter()
	if err := printer.Print(&buf, file); err != nil {
		return astResult{recognized: false, rejectCause: "shell print error: " + err.Error()}
	}
	out := buf.String()
	// syntax.Printer always terminates with a newline; the input rarely has
	// one. Strip one trailing newline so the rewrite round-trips for typical
	// single-line inputs and our `bash -n` regression smoke check.
	out = strings.TrimRight(out, "\n")

	return astResult{
		recognized: true,
		rewritten:  out,
		addedA:     addedA,
		addedB:     addedB,
	}
}

// findCommitCallExprs walks the AST and returns every CallExpr whose head
// resolves to `git` (possibly with env-var prefix assignments) and whose
// first positional argument is the literal `commit`.
//
// If the walker encounters an unsupported construct that wraps or might be a
// real commit (eval-builtin, dynamic command head, bash -c with a shell
// string), it returns cause set to a short string identifying it; nodes is
// nil in that case.
func findCommitCallExprs(file *syntax.File) (nodes []*syntax.CallExpr, cause string) {
	var blockerCause string
	syntax.Walk(file, func(node syntax.Node) bool {
		if blockerCause != "" {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		// Need at least the command name itself.
		if len(call.Args) == 0 {
			return true
		}
		head := call.Args[0]
		headLit, isLit := wordToLiteral(head)
		if !isLit {
			// Head is a substitution or expansion (e.g. $(echo git)).
			// We can't reason about which command this resolves to, so any
			// dynamic head must block the gate — but only flag it as a
			// blocker if it LOOKS like a real command invocation (i.e. we
			// are not inside a quoted-string mention). The walker visits
			// CallExpr nodes only for real call positions, so it is safe
			// to flag here.
			blockerCause = "dynamic command name"
			return false
		}
		switch headLit {
		case "eval":
			blockerCause = "eval"
			return false
		case "bash", "sh", "zsh", "dash":
			// Inspect args for -c followed by a shell string: that is a
			// dynamic shell that we cannot statically rewrite.
			for i := 1; i < len(call.Args); i++ {
				lit, ok := wordToLiteral(call.Args[i])
				if !ok {
					continue
				}
				if lit == "-c" && i+1 < len(call.Args) {
					blockerCause = headLit + " -c"
					return false
				}
			}
			return true
		case "git":
			// Locate the first positional argument that is NOT an option/flag
			// argument to git itself. `git -C <path> commit` puts a flag and
			// its value before `commit`.
			sub, found := gitSubcommand(call.Args[1:])
			if found && sub == "commit" {
				nodes = append(nodes, call)
			}
			return true
		default:
			// Exec-wrappers run their remaining arguments as a command:
			// `xargs git commit`, `env git commit`, `sudo git commit`, etc.
			// Here `git` and `commit` are positional args of the wrapper, not a
			// nested CallExpr, so the `git` case above never sees them and the
			// commit would slip through ungated. We cannot statically splice a
			// trailer into an argv passed to a wrapper, so block with a named
			// cause. Restricted to known wrappers so inert mentions like
			// `echo git commit` (echo does not exec its args) stay allowed.
			if isExecWrapper(headLit) && hasAdjacentGitCommit(call.Args) {
				blockerCause = "git commit via " + headLit
				return false
			}
			return true
		}
	})
	if blockerCause != "" {
		return nil, blockerCause
	}
	return nodes, ""
}

// execWrappers are commands that execute their trailing arguments as a new
// command. A `git commit` appearing among their args is a real commit the
// fast-path matcher and the `git` CallExpr case both miss.
var execWrappers = map[string]bool{
	"xargs": true, "env": true, "sudo": true, "doas": true, "nohup": true,
	"command": true, "timeout": true, "nice": true, "ionice": true,
	"stdbuf": true, "setsid": true, "chronic": true, "flock": true,
}

// isExecWrapper reports whether name runs its trailing arguments as a command.
func isExecWrapper(name string) bool { return execWrappers[name] }

// hasAdjacentGitCommit reports whether args contains a literal `git`
// immediately followed by a literal `commit` (the argv shape of an indirect
// commit). Quoted whole-string mentions like `grep "git commit"` reduce to a
// single arg and never match, so this does not false-positive on them.
func hasAdjacentGitCommit(args []*syntax.Word) bool {
	for i := 0; i+1 < len(args); i++ {
		a, okA := wordToLiteral(args[i])
		if !okA || a != "git" {
			continue
		}
		b, okB := wordToLiteral(args[i+1])
		if okB && b == "commit" {
			return true
		}
	}
	return false
}

// gitSubcommand scans the args slice (args AFTER `git` itself) and returns
// the git subcommand. It skips the small set of `git` global flags that take
// a value (-C path, -c key=value, --git-dir=, --work-tree=, etc.).
//
// Returns ok=false if no subcommand is found or any argument has a dynamic
// (non-literal) value where we'd need to interpret it. Conservative: if we
// can't tell, fall through to "not commit" rather than mis-splice.
func gitSubcommand(args []*syntax.Word) (string, bool) {
	i := 0
	for i < len(args) {
		w := args[i]
		lit, ok := wordToLiteral(w)
		if !ok {
			// Dynamic positional we can't interpret. Bail.
			return "", false
		}
		// Long-form flags with embedded `=` consume themselves only.
		if strings.HasPrefix(lit, "--") && strings.Contains(lit, "=") {
			i++
			continue
		}
		// Short and long flags taking a separate value argument.
		switch lit {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace",
			"--super-prefix", "--exec-path":
			i += 2
			continue
		}
		// Standalone flag with no value.
		if strings.HasPrefix(lit, "-") {
			i++
			continue
		}
		return lit, true
	}
	return "", false
}

// wordToLiteral reduces a syntax.Word to a single literal string when every
// part is a literal or single-quoted segment. Returns ok=false on any
// dynamic part (substitution, double-quoted with expansion, parameter
// expansion, etc.). Double-quoted strings whose parts are all literals are
// accepted — they're just quoting.
func wordToLiteral(w *syntax.Word) (string, bool) {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}

// callExprHasTrailerValue reports whether the given commit CallExpr already
// carries a --trailer (or --trailer=) argument whose value contains marker.
// Mirrors the string-based HasTrailerA / HasTrailerB shape so idempotency
// behavior matches between the fast and AST paths.
func callExprHasTrailerValue(call *syntax.CallExpr, marker string) bool {
	args := call.Args
	for i := 0; i < len(args); i++ {
		lit, ok := wordToLiteral(args[i])
		if !ok {
			continue
		}
		if lit == "--trailer" && i+1 < len(args) {
			val, ok := wordToLiteral(args[i+1])
			if ok && strings.Contains(val, marker) {
				return true
			}
			continue
		}
		if strings.HasPrefix(lit, "--trailer=") {
			val := strings.TrimPrefix(lit, "--trailer=")
			if strings.Contains(val, marker) {
				return true
			}
		}
	}
	// Also scan -m / --message values for trailers already in the body.
	msg := extractMessageFromCall(call)
	if msg != "" && messageHasTrailer(msg, marker) {
		return true
	}
	return false
}

// extractMessageFromCall pulls the -m / --message value(s) out of a commit
// CallExpr as a single concatenated string (separated by blank lines, the
// same way git does).
func extractMessageFromCall(call *syntax.CallExpr) string {
	var msg string
	args := call.Args
	for i := 0; i < len(args); i++ {
		lit, ok := wordToLiteral(args[i])
		if !ok {
			continue
		}
		var val string
		var got bool
		switch {
		case lit == "-m" || lit == "--message":
			if i+1 < len(args) {
				val, got = wordToLiteral(args[i+1])
			}
		case strings.HasPrefix(lit, "--message="):
			val = strings.TrimPrefix(lit, "--message=")
			got = true
		}
		if got {
			if msg == "" {
				msg = val
			} else {
				msg = msg + "\n\n" + val
			}
		}
	}
	return msg
}

// appendTrailerToCall appends `--trailer '<value>'` to the args of call. The
// trailer value is wrapped in a single-quoted Word, which preserves it
// verbatim regardless of embedded $, backticks, double quotes, or newlines.
func appendTrailerToCall(call *syntax.CallExpr, value string) {
	flagWord := &syntax.Word{Parts: []syntax.WordPart{
		&syntax.Lit{Value: "--trailer"},
	}}
	valueWord := &syntax.Word{Parts: []syntax.WordPart{
		&syntax.SglQuoted{Value: value},
	}}
	call.Args = append(call.Args, flagWord, valueWord)
}

// formatAstRejectStderr produces a stderr message naming the cause of an AST
// rejection. The cause string comes from findCommitCallExprs and is short
// enough to splice into a single line.
func formatAstRejectStderr(cause string) string {
	return fmt.Sprintf("pakka: commit gate cannot rewrite this shape — %s is not supported. Run the commit on its own (no chained shell substitution) or add [skip pakka] to bypass.", cause)
}

// hasRawSkipMarker reports whether the literal [skip pakka] marker appears
// anywhere in the raw command text. Used only on AST reject paths (shell
// parse errors and blocked shapes like eval / bash -c / dynamic command
// heads) where -m message extraction fails, so HasSkipMarker's positional
// rules cannot run. The reject stderr advertises the marker as a bypass, so
// it must work on exactly these paths; a whole-string scan is the simplest
// way to honor an explicit user opt-out we cannot parse. Recognized shapes
// keep HasSkipMarker's stricter positional semantics.
func hasRawSkipMarker(cmd string) bool {
	return strings.Contains(cmd, "[skip pakka]")
}

// skipMarkerDecision is the allow decision for an explicit [skip pakka]
// opt-out: gate, trailers, and audit all bypassed, with the skip surfaced on
// stderr and recorded in the audit note. Shared by the recognized-shape path
// and the reject paths so the bypass behaves identically everywhere.
func skipMarkerDecision() *Decision {
	return &Decision{
		Allow:     true,
		IsCommit:  true,
		AuditNote: "review_skipped=skip_marker",
		Stderr:    "pakka: [skip pakka] detected — gate, trailers, and audit bypassed for this commit",
	}
}

// evaluateViaAST is the AST-based decision path. Invoked from Evaluate when
// the fast-path string matcher misses but the raw command mentions
// `git commit` anywhere (real token, quoted string, substitution). Parses
// the command, walks every CallExpr, and either splices trailers, blocks
// with a named cause (eval / dynamic head / bash -c), or passes through
// when the mention turns out to be inert (e.g. `grep "git commit" file`).
//
// Gate application: runs ONLY when the AST located at least one real commit
// node. Otherwise the routing was a false alarm and the command is allowed
// untouched — applying the gate to non-commit commands would surface
// "review gate active" errors on innocuous greps, breaking established
// `TestEvaluate_SubstringFallback_AllowsQuotedMentions` behavior.
func evaluateViaAST(cmd string, cfg *Config, state *State) *Decision {
	// Probe the AST FIRST so we know whether this is actually a commit
	// before deciding whether the gate fires.
	r := strings.NewReader(cmd)
	parser := syntax.NewParser(syntax.KeepComments(true))
	file, parseErr := parser.Parse(r, "")
	if parseErr != nil {
		// Couldn't even parse. The reject stderr advertises [skip pakka] as
		// a bypass, so honor it before blocking (issue #8): with no parseable
		// message for HasSkipMarker's positional rules, fall back to the raw
		// text scan. A commit is plausibly present — this path only routes on
		// a `git commit` mention — so the skip decision flags IsCommit.
		if hasRawSkipMarker(cmd) {
			return skipMarkerDecision()
		}
		// Conservative block — name the cause so the model can rewrite the
		// command.
		return &Decision{Allow: false, Stderr: formatAstRejectStderr("shell parse error")}
	}

	commitNodes, blockerCause := findCommitCallExprs(file)
	if blockerCause != "" {
		// eval / dynamic command name / bash -c with shell string — any of
		// these wrap a potential commit in a way that we cannot statically
		// inject trailers into. Honor an explicit [skip pakka] first (issue
		// #8): -m extraction rarely works on these shapes (the message hides
		// inside a quoted shell string), so the raw text scan stands in for
		// HasSkipMarker here. Otherwise block with the cause spelled out.
		if hasRawSkipMarker(cmd) {
			return skipMarkerDecision()
		}
		return &Decision{Allow: false, Stderr: formatAstRejectStderr(blockerCause)}
	}
	if len(commitNodes) == 0 {
		// Substring routing fired but the AST sees no real commit node
		// (e.g. `grep "git commit" file.go`). Pass through untouched.
		return &Decision{Allow: true}
	}

	// Confirmed: at least one real commit invocation present. From here
	// the logic mirrors the fast-path Evaluate flow — skip-marker, then
	// trailer/gate decisions. Every return below sets IsCommit so the caller
	// writes a verdict / audit entry for chained and wrapped commits too.
	if !cfg.Signature && !cfg.CoAuthor && !cfg.AutoGate {
		return &Decision{Allow: true, IsCommit: true}
	}

	if HasSkipMarker(cmd) {
		return skipMarkerDecision()
	}

	wantA := cfg.Signature
	wantB := cfg.CoAuthor

	gateBlocks := false
	auditNote := ""
	trailerA := BaselineTrailer(cfg.Version, cfg.SessionID)

	if cfg.AutoGate {
		switch {
		case cfg.MaxDiffBytes > 0 && state.DiffBytes > cfg.MaxDiffBytes:
			auditNote = "review_skipped=oversize"
		case state.HasRecentPass:
			trailerA = StrongTrailer(cfg.Version, cfg.SessionID)
		case len(state.ErrorFindings) > 0:
			gateBlocks = true
		default:
			gateBlocks = true
		}
	}

	if gateBlocks {
		if len(state.ErrorFindings) > 0 {
			return &Decision{Allow: false, IsCommit: true, Stderr: FormatFindings(state.ErrorFindings)}
		}
		return &Decision{
			Allow:    false,
			IsCommit: true,
			Stderr:   "pakka: review gate active. No passing review found.\nRun /pakka:review on staged changes, or add [skip pakka] to bypass.",
		}
	}

	// Splice trailers into the commit nodes we already located. The
	// astInjectTrailers helper re-parses the command to keep one source
	// of truth for traversal/printing — the small re-parse cost is
	// negligible vs. the work AST splicing replaces.
	res := astInjectTrailers(cmd, wantA, trailerA, wantB, CoAuthorTrailer())
	if !res.recognized {
		if res.rejectCause != "" {
			return &Decision{Allow: false, Stderr: formatAstRejectStderr(res.rejectCause)}
		}
		// Should not happen — we already verified commit nodes exist on
		// the first parse — but guard for safety.
		return &Decision{Allow: false, Stderr: formatAstRejectStderr("unexpected: commit node disappeared on re-parse")}
	}

	d := &Decision{Allow: true, IsCommit: true, AuditNote: auditNote}
	if res.addedA || res.addedB {
		d.Command = res.rewritten
	}
	return d
}
