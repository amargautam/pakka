// Linguistic compression: drops articles, fillers, hedging, pleasantries,
// while preserving code, URLs, identifiers, and numeric/marker tokens.
// Applied after structural compression in strict mode. Deterministic,
// rule-based, idempotent.
package compress

import (
	"regexp"
	"strconv"
	"strings"
)

// protectedPatterns match spans that must not be modified by linguistic rules.
// Order matters: earlier patterns take precedence via placeholder replacement.
var protectedPatterns = []*regexp.Regexp{
	regexp.MustCompile("`[^`]+`"),                   // inline code
	regexp.MustCompile(`https?://\S+`),              // URLs
	regexp.MustCompile(`~/[\w./+-]+`),               // ~/paths
	regexp.MustCompile(`\./[\w./+-]+`),              // ./paths
	regexp.MustCompile(`/[\w.-]+(?:/[\w.-]+)+`),     // absolute: /usr/bin
	regexp.MustCompile(`\b[\w.-]+(?:/[\w.-]+){2,}`), // multi-segment: a/b/c
	regexp.MustCompile(`\b[\w-]+/[\w.-]+\.\w+`),    // with ext: foo/bar.go
	regexp.MustCompile(`\b\w+_\w+\b`),              // underscore_ids
	regexp.MustCompile(`\b[a-z]+[A-Z]\w*\b`),       // camelCase
	regexp.MustCompile(`\b\d[\d.]*\w*\b`),          // numbers: 42, 3.14, 2KB
	regexp.MustCompile(`(?i)\b(?:TODO|FIXME|SECURITY|HACK|BUG|XXX)\b`), // markers
	regexp.MustCompile(`\b[A-Z][\w]*-[\d.]+\b`),    // SPDX: Apache-2.0
}

// linguisticRules are applied in order after protecting special spans.
// Multi-word phrases before single words to prevent partial matches.
var linguisticRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	// 5. Fragment: drop leading phrases
	{regexp.MustCompile(`(?i)^That is\s+`), ""},
	{regexp.MustCompile(`(?i)^This is\s+`), ""},
	{regexp.MustCompile(`(?i)^There (?:is|are)\s+`), ""},
	{regexp.MustCompile(`(?i)^It is\s+`), ""},

	// 3. Hedging
	{regexp.MustCompile(`(?i)\bI think\s+`), ""},
	{regexp.MustCompile(`(?i)\bI believe\s+`), ""},
	{regexp.MustCompile(`(?i)\bin my opinion,?\s*`), ""},
	{regexp.MustCompile(`(?i)\bit seems\s+`), ""},
	// 4. Pleasantries
	{regexp.MustCompile(`(?i)\bplease\s+`), ""},
	{regexp.MustCompile(`(?i)\bthanks\.?\s*`), ""},
	{regexp.MustCompile(`(?i)\blet me know\.?\s*`), ""},
	{regexp.MustCompile(`(?i)\bhappy to\s+`), ""},

	// 2. Filler (multi-word first)
	{regexp.MustCompile(`(?i)\bkind of\s+`), ""},
	{regexp.MustCompile(`(?i)\bsort of\s+`), ""},
	{regexp.MustCompile(`(?i)\bjust\s+`), ""},
	{regexp.MustCompile(`(?i)\breally\s+`), ""},
	{regexp.MustCompile(`(?i)\bbasically,?\s*`), ""},
	{regexp.MustCompile(`(?i)\bsimply\s+`), ""},
	{regexp.MustCompile(`(?i)\bvery\s+`), ""},
	{regexp.MustCompile(`(?i)\bactually,?\s*`), ""},

	// 1. Articles (last — after phrases containing them)
	{regexp.MustCompile(`(?i)\bthe\s+`), ""},
	{regexp.MustCompile(`(?i)\ban\s+`), ""},
	{regexp.MustCompile(`\ba\s+`), ""},
}

// applyLinguistic runs word-level compression on already-structurally-compressed text.
// Called only in strict mode.
func applyLinguistic(input string) string {
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	inCode := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCode = !inCode
			out = append(out, line)
			continue
		}
		if inCode || trimmed == "" {
			out = append(out, line)
			continue
		}
		out = append(out, linguisticLine(line))
	}

	return strings.Join(out, "\n")
}

// linguisticLine compresses a single non-code, non-empty line.
func linguisticLine(line string) string {
	// Protect special spans with NUL-delimited placeholders: \x00p<idx>\x00.
	// NUL bytes do not appear in normal markdown text. The "p" prefix keeps
	// the decimal index away from a word boundary, so subsequent protected
	// patterns (notably `\b\d[\d.]*\w*\b` and `\b[a-z]+[A-Z]\w*\b`) and the
	// linguistic drop rules cannot re-match a placeholder. The decimal index
	// has no upper bound, so this scheme survives lines with thousands of
	// protected spans (the previous PUA scheme U+E000+ overflowed at 6400).
	var saved []string
	for _, re := range protectedPatterns {
		line = re.ReplaceAllStringFunc(line, func(m string) string {
			idx := len(saved)
			saved = append(saved, m)
			return "\x00p" + strconv.Itoa(idx) + "\x00"
		})
	}

	// Apply drop rules in order
	for _, rule := range linguisticRules {
		line = rule.re.ReplaceAllString(line, rule.repl)
	}

	// Restore protected spans
	for i, s := range saved {
		line = strings.Replace(line, "\x00p"+strconv.Itoa(i)+"\x00", s, 1)
	}

	// Collapse double spaces, trim
	line = multiSpace.ReplaceAllString(line, " ")
	return strings.TrimSpace(line)
}
