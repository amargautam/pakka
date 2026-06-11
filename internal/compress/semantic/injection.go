// Injection detection for semantic rewrites — output-poisoning hardening
// (issue #15).
//
// The rewriter subprocess runs sandboxed (no tools), so injected content in a
// compressed file cannot execute anything directly. The residual risk is
// OUTPUT POISONING: instructions smuggled into the file steer the rewrite,
// and the poisoned compressed text is then trusted by the main session.
//
// DetectInjection rejects rewriter output containing instruction-shaped
// ADDITIONS absent from the input: imperatives addressed to the assistant,
// tool names, hook keywords, skill-invocation strings, and new code fences.
// The delta is what matters — pakka's own docs legitimately contain tool
// names and hook keywords, so output that PRESERVES them is fine; output
// that ADDS them is rejected.
//
// Detection is deterministic and table-driven. Zero LLM calls.
package semantic

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// KindInjectionAdded is the Violation kind reported for every
// instruction-shaped span present in the rewritten output more often than in
// the original input. Stable across releases — used by audit consumers and
// by tests asserting which rule fired.
const KindInjectionAdded = "injection-added"

// ErrInjectionSuspect is wrapped by RunSemantic when the rewriter output
// contains instruction-shaped additions. Callers MUST discard the output and
// fall back to deterministic compression.
var ErrInjectionSuspect = errors.New("semantic: instruction-shaped additions in rewriter output")

// InjectionError is returned when DetectInjection finds additions. Unlike
// *FailedError there is no cherry-pick retry — a rewrite that smuggles
// instructions does not get more chances; callers receive the original input
// unchanged and should fall back to the deterministic engine.
type InjectionError struct {
	Findings []Violation
}

// Error implements the error interface.
//
// Purpose: Stable, low-cardinality message for logs (finding count only,
// never the smuggled content verbatim at full length).
// Errors: None.
func (e *InjectionError) Error() string {
	return fmt.Sprintf("%s (%d findings)", ErrInjectionSuspect.Error(), len(e.Findings))
}

// Unwrap exposes ErrInjectionSuspect for errors.Is checks.
func (e *InjectionError) Unwrap() error { return ErrInjectionSuspect }

// Violations returns the injection findings.
//
// Purpose: Accessor used by callers (orchestrator audit entry, tests).
// Errors: None.
func (e *InjectionError) Violations() []Violation { return e.Findings }

// injectionPattern is one row of the detection table.
type injectionPattern struct {
	// name labels the rule in finding excerpts and debug logs.
	name string
	re   *regexp.Regexp
	// fold lowercases matched spans before tallying so case-insensitive
	// patterns count "You must" and "you must" as the same span.
	fold bool
}

// injectionPatterns is the detection table. Ordering is part of the contract:
// findings are emitted in table order (then sorted span order), so output is
// deterministic for identical inputs.
//
// Two classes of tool-name rule:
//   - tool-name: unambiguous identifiers (Bash, WebFetch, ...) matched bare,
//     case-sensitively.
//   - tool-directive / tool-suffix: tool names that are also common English
//     words (Write, Edit, Read, Task, Skill, Agent) only count in
//     instruction shape ("use the Edit tool", "run Read"); bare prose like
//     "Edit history is preserved" must not fire.
var injectionPatterns = []injectionPattern{
	// Imperatives addressed to the assistant.
	{name: "imperative-assistant", re: regexp.MustCompile(`(?i)\byou (?:must|should now|are now|will now|have to)\b`), fold: true},
	{name: "imperative-override", re: regexp.MustCompile(`(?i)\b(?:ignore|disregard|forget|override)\s+(?:all\s+|any\s+|the\s+|your\s+)?(?:previous|prior|above|earlier|preceding|original|system)\b`), fold: true},
	{name: "imperative-execute", re: regexp.MustCompile(`(?i)\b(?:run|execute) the following\b`), fold: true},
	{name: "imperative-invoke", re: regexp.MustCompile(`(?i)\binvoke\b`), fold: true},
	{name: "imperative-new-instructions", re: regexp.MustCompile(`(?i)\bnew instructions?\b`), fold: true},
	{name: "imperative-system-prompt", re: regexp.MustCompile(`(?i)\bsystem prompt\b`), fold: true},
	{name: "imperative-conceal", re: regexp.MustCompile(`(?i)\bdo not (?:tell|inform|alert) the user\b`), fold: true},
	// Tool names — unambiguous identifiers, case-sensitive.
	{name: "tool-name", re: regexp.MustCompile(`\b(?:Bash|WebFetch|WebSearch|NotebookEdit|MultiEdit|TodoWrite|BashOutput|KillShell|SlashCommand|ExitPlanMode|AskUserQuestion|Grep|Glob)\b`)},
	// Tool names that are common English words — directive shape only.
	{name: "tool-directive", re: regexp.MustCompile(`\b(?i:use|call|invoke|run|launch|open)\s+(?i:the\s+)?(?:Write|Edit|Read|Task|Skill|Agent)\b`)},
	{name: "tool-suffix", re: regexp.MustCompile(`\b(?:Write|Edit|Read|Task|Skill|Agent)\s+(?i:tool)\b`)},
	// Hook keywords.
	{name: "hook-keyword", re: regexp.MustCompile(`\b(?:SessionStart|SessionEnd|PreToolUse|PostToolUse|UserPromptSubmit|SubagentStop|PreCompact)\b`)},
	// Skill-invocation strings.
	{name: "skill-invocation", re: regexp.MustCompile(`(?i)/pakka:[a-z][a-z-]*`), fold: true},
	// New code fences — the rewriter must never introduce executable-looking
	// blocks that were not in the input. (The Validator already guarantees
	// existing blocks are preserved; this catches additions.)
	{name: "code-fence", re: regexp.MustCompile("(?m)^(?:```|~~~)")},
}

// DetectInjection reports instruction-shaped spans that appear MORE often in
// rewritten than in original. Per-span delta counting means legitimately
// preserved keywords never fire; only additions do.
//
// Purpose: Deterministic injection gate for semantic rewrites — rejects
// output poisoning before the compressed text is adopted.
// Errors: None — pure function over strings, zero LLM calls.
func DetectInjection(original, rewritten string) []Violation {
	if rewritten == "" {
		return nil
	}
	var out []Violation
	for _, p := range injectionPatterns {
		origCounts := tallyMatches(p, original)
		gotCounts := tallyMatches(p, rewritten)
		spans := make([]string, 0, len(gotCounts))
		for span := range gotCounts {
			spans = append(spans, span)
		}
		sort.Strings(spans)
		for _, span := range spans {
			added := gotCounts[span] - origCounts[span]
			for i := 0; i < added; i++ {
				out = append(out, Violation{
					Kind:    KindInjectionAdded,
					Excerpt: excerpt(p.name + ": " + span),
				})
			}
		}
	}
	return out
}

// tallyMatches counts occurrences of each matched span in s, folding case
// when the pattern is case-insensitive.
func tallyMatches(p injectionPattern, s string) map[string]int {
	counts := make(map[string]int)
	for _, m := range p.re.FindAllString(s, -1) {
		if p.fold {
			m = strings.ToLower(m)
		}
		counts[m]++
	}
	return counts
}
