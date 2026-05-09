// Validator preserves load-bearing regions across an LLM rewrite. It extracts
// the spans the rewriter MUST NOT modify (code, URLs, paths, dates, versions,
// env vars, critical markers) from the original and confirms each appears
// verbatim in the rewritten output. Anything missing is reported as a
// Violation with a kind tag and a short excerpt for debug logging.
//
// The validator is deterministic and side-effect free. It is the gate that
// keeps semantic mode safe.
package semantic

import (
	"regexp"
	"strings"
)

// Violation is one preservation failure: a region that existed in the
// original but is missing (or altered) in the rewritten text.
type Violation struct {
	// Kind names the preservation rule that failed.
	Kind string `json:"kind"`
	// Excerpt is a ≤120-char view of the missing region, for debug logging.
	Excerpt string `json:"excerpt"`
}

// Violation Kind constants. Stable across releases — used by retry prompts
// and by tests asserting which rule fired.
const (
	KindCodeBlockModified  = "code-block-modified"
	KindInlineCodeChanged  = "inline-code-changed"
	KindURLChanged         = "url-changed"
	KindPathChanged        = "path-changed"
	KindDateChanged        = "date-changed"
	KindVersionChanged     = "version-changed"
	KindEnvVarChanged      = "env-var-changed"
	KindCriticalMarkerLost   = "critical-marker-lost"
	KindNegationLost         = "negation-lost"
	KindPercentageChanged    = "percentage-changed"
)

// excerpt returns at most 120 chars of s for logging, with newlines collapsed
// to single spaces and surrounding whitespace trimmed.
func excerpt(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}

// Region patterns. Each must extract spans whose textual content is
// load-bearing — the rewriter is told never to alter them, and the validator
// confirms each appears verbatim downstream.
//
// Multiline patterns use (?s) where dot must match newlines.
var (
	reFencedTriple = regexp.MustCompile("(?s)```[a-zA-Z0-9_+-]*\\n.*?\\n```")
	reFencedTilde  = regexp.MustCompile("(?s)~~~[a-zA-Z0-9_+-]*\\n.*?\\n~~~")
	// reInlineCode requires ≥2 non-backtick, non-newline chars between the
	// fences. Single-char spans like `a` or `i` are everyday English usage,
	// not load-bearing identifiers — matching them produces false-positive
	// violations that exhaust the validator's cherry-pick retry budget.
	reInlineCode   = regexp.MustCompile("`[^`\n]{2,}`")
	reURL          = regexp.MustCompile(`(?:https?|ftp|ssh)://[^\s)]+`)
	// Path heuristics. Tightened to avoid matching ordinary words.
	rePathAbs    = regexp.MustCompile(`(?:^|[\s(\[])(/[A-Za-z0-9_.][\w./-]*)`)
	rePathHome   = regexp.MustCompile(`(?:^|[\s(\[])(~/[\w./-]+)`)
	rePathRel    = regexp.MustCompile(`(?:^|[\s(\[])(\./[\w./-]+)`)
	rePathWin    = regexp.MustCompile(`[A-Za-z]:\\[\w\\.-]+`)
	reISODate    = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	reVersion    = regexp.MustCompile(`\bv?\d+\.\d+(?:\.\d+)?\b`)
	reEnvVar     = regexp.MustCompile(`\$[A-Z_][A-Z0-9_]*`)
	reMarker     = regexp.MustCompile(`\b(?:TODO|FIXME|SECURITY|HACK|BUG|XXX)\b`)
	// reNegation matches negation words whose removal or alteration inverts meaning.
	// Case-insensitive: "Not", "NOT", "not" all load-bearing.
	reNegation = regexp.MustCompile(`(?i)\b(?:not|never|no|cannot|can't|shouldn't|won't|don't|isn't|aren't|wasn't|weren't|nor)\b`)
	// rePercent matches numeric percentages. Handles decimals (99.9%).
	rePercent  = regexp.MustCompile(`\b\d+(?:\.\d+)?%`)
)

// extractAll runs re over s and returns the captured strings. When the regexp
// has a single sub-group, the first sub-match is used; otherwise the whole
// match. Duplicates are preserved (each occurrence must survive the rewrite).
func extractAll(re *regexp.Regexp, s string) []string {
	matches := re.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) >= 2 && m[1] != "" {
			out = append(out, m[1])
			continue
		}
		out = append(out, m[0])
	}
	return out
}

// containsCount reports how many times needle appears in haystack. Empty
// needle returns 0 to keep the contains-at-least-as-many invariant simple.
func containsCount(haystack, needle string) int {
	if needle == "" {
		return 0
	}
	count := 0
	idx := 0
	for {
		i := strings.Index(haystack[idx:], needle)
		if i < 0 {
			return count
		}
		count++
		idx += i + len(needle)
	}
}

// checkRegion adds a Violation to out for every occurrence of a region in
// original whose count exceeds the count in rewritten. Each occurrence is
// reported separately so a missing 2-of-3 yields two Violations with kind=k.
func checkRegion(out []Violation, k, original, rewritten string, re *regexp.Regexp) []Violation {
	matches := extractAll(re, original)
	// Tally so duplicate spans (e.g. same URL twice) are only over-flagged
	// when at least one occurrence really is missing downstream.
	origCounts := make(map[string]int)
	for _, m := range matches {
		origCounts[m]++
	}
	for span, want := range origCounts {
		got := containsCount(rewritten, span)
		if got >= want {
			continue
		}
		missing := want - got
		for i := 0; i < missing; i++ {
			out = append(out, Violation{Kind: k, Excerpt: excerpt(span)})
		}
	}
	return out
}

// Validate reports preservation violations between the original and rewritten
// strings. Returns nil when every preservable region survived verbatim.
//
// Purpose: Deterministic gate that prevents the LLM rewriter from corrupting
// load-bearing context (code, URLs, paths, dates, versions, env vars,
// critical markers).
// Errors: None — pure function over strings.
func Validate(original, rewritten string) []Violation {
	if original == "" {
		return nil
	}

	var out []Violation

	// Code blocks (triple + tilde) and inline backticks.
	out = checkRegion(out, KindCodeBlockModified, original, rewritten, reFencedTriple)
	out = checkRegion(out, KindCodeBlockModified, original, rewritten, reFencedTilde)
	out = checkRegion(out, KindInlineCodeChanged, original, rewritten, reInlineCode)

	// URLs.
	out = checkRegion(out, KindURLChanged, original, rewritten, reURL)

	// Paths (POSIX absolute, ~/, ./, Windows).
	out = checkRegion(out, KindPathChanged, original, rewritten, rePathAbs)
	out = checkRegion(out, KindPathChanged, original, rewritten, rePathHome)
	out = checkRegion(out, KindPathChanged, original, rewritten, rePathRel)
	out = checkRegion(out, KindPathChanged, original, rewritten, rePathWin)

	// ISO dates and version strings.
	out = checkRegion(out, KindDateChanged, original, rewritten, reISODate)
	out = checkRegion(out, KindVersionChanged, original, rewritten, reVersion)

	// Environment variables.
	out = checkRegion(out, KindEnvVarChanged, original, rewritten, reEnvVar)

	// Critical markers — uppercase only; the deterministic engine already
	// preserves lines containing these. Semantic mode must too.
	out = checkRegion(out, KindCriticalMarkerLost, original, rewritten, reMarker)

	// Negation words — removal inverts meaning of policy/security statements.
	out = checkRegion(out, KindNegationLost, original, rewritten, reNegation)

	// Percentages — LLM can silently substitute numeric values.
	out = checkRegion(out, KindPercentageChanged, original, rewritten, rePercent)

	return out
}
