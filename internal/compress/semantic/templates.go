// Prompt templates for semantic compression. One template per Level. Each
// carries explicit preserve-verbatim constraints for the regions the
// validator checks, plus a worked example that anchors the level's
// aggressiveness.
//
// Templates accept a single {{.Input}} placeholder. They are pakka's own
// voice — no third-party branding (see DECISIONS.md scrub rule).
package semantic

import (
	"bytes"
	"fmt"
	"text/template"
)

// preserveBlock is shared across every level — the validator gates on these
// regions, so the prompt must teach the same rules.
const preserveBlock = `## Preserve verbatim — never modify
- Fenced code blocks (` + "```" + ` and ~~~), entire content + opening fence + language tag.
- Inline backtick spans.
- URLs (http://, https://, ftp://, ssh://).
- File paths (/abs, ~/home, ./rel, C:\windows).
- Shell commands (typically inside backticks).
- Dates in YYYY-MM-DD form.
- Version strings (v1.2.3, 0.1.0, 1.23).
- Environment variables ($FOO, $PAKKA_X).
- Library/product proper nouns (Go, TypeScript, Anthropic, Claude Code, Apache-2.0).
- Critical markers (TODO, FIXME, SECURITY, HACK, BUG, XXX) and the lines they sit on.

If you cannot rewrite a sentence without altering one of the above, leave the sentence alone.
`

const liteTmpl = `Rewrite the input below for compactness, level=lite.

Goal: drop filler, hedging, pleasantries. Keep articles. Keep grammar correct sentences.
Drop: just, really, basically, actually, simply, very, kind of, sort of, of course,
sure, certainly, happy to, I think, I believe, perhaps, maybe, in my opinion.

` + preserveBlock + `

Worked example:
INPUT:  I think we should probably just refactor the auth module, since it really has too many edge cases that basically all stem from the same root cause.
OUTPUT: We should refactor the auth module; it has too many edge cases stemming from the same root cause.

Now rewrite the input. Output ONLY the rewritten text. No preamble, no closing remarks.

INPUT:
{{.Input}}
`

const strictTmpl = `Rewrite the input below for compactness, level=strict.

Goal: drop articles (a/an/the), filler, hedging, pleasantries. Fragments are fine.
Use short synonyms (big over extensive, fix over implement-a-solution-for, use over utilize).
Keep technical terms exact.

` + preserveBlock + `

Worked example:
INPUT:  The auth middleware has a bug. The token expiry check is using a strict less-than comparison, when it should be using less-than-or-equal. The fix is small.
OUTPUT: Auth middleware bug. Token expiry uses < not <=. Fix: small.

Now rewrite the input. Output ONLY the rewritten text. No preamble, no closing remarks.

INPUT:
{{.Input}}
`

const ultraTmpl = `Rewrite the input below for compactness, level=ultra.

Goal: extreme density. Strict rules plus:
- Abbreviate routinely: DB, auth, config, req, res, fn, impl, env, repo, info, init.
- Strip conjunctions where dropping does not break meaning.
- Use arrows for causality: "X leads to Y" -> "X -> Y".
- Drop linking verbs where a noun phrase is enough.
- One word over two when one suffices.

` + preserveBlock + `

Worked example:
INPUT:  When the request comes in, the authentication middleware checks the token, and if the token is expired, it returns a 401 response to the client.
OUTPUT: Req in -> auth middleware checks token -> if expired, return 401.

Now rewrite the input. Output ONLY the rewritten text. No preamble, no closing remarks.

INPUT:
{{.Input}}
`

const superUltraTmpl = `Rewrite the input below for compactness, level=super-ultra.

Goal: maximum density. Ultra rules plus:
- One token where one suffices. Drop every non-load-bearing word.
- Use symbols: -> for "leads to / causes", = for "equals / is", & for "and".
- Drop pronouns and possessives where context disambiguates.
- Use sentence fragments freely. Bullet-style is fine. Tables are great.
- Keep numbers, identifiers, paths, code, URLs, version strings exact.

` + preserveBlock + `

Worked example:
INPUT:  When a user submits a request that has an expired token, the authentication middleware will reject the request and return an HTTP 401 status code to the client.
OUTPUT: Expired token -> auth middleware rejects -> 401.

Now rewrite the input. Output ONLY the rewritten text. No preamble, no closing remarks.

INPUT:
{{.Input}}
`

// templateMap is the source-of-truth registry. levelTemplate selects from it.
var templateMap = map[Level]string{
	LevelLite:       liteTmpl,
	LevelStrict:     strictTmpl,
	LevelUltra:      ultraTmpl,
	LevelSuperUltra: superUltraTmpl,
}

// levelTemplate returns the raw template body for a Level.
//
// Purpose: Expose template text for unit tests and for callers building
// alternative prompt wrappers.
// Errors: Returns the strict template when the Level is unknown — every Level
// in the type switch is registered, so this branch is defensive.
func levelTemplate(l Level) string {
	if t, ok := templateMap[l]; ok {
		return t
	}
	return strictTmpl
}

// renderPrompt renders the level's template with the supplied input.
//
// Purpose: Produce the full prompt body sent to the Rewriter.
// Errors: Returns an error only if the template fails to compile, which can
// happen for hand-crafted templates injected via tests.
func renderPrompt(l Level, input string) (string, error) {
	body := levelTemplate(l)
	t, err := template.New(string(l)).Parse(body)
	if err != nil {
		return "", fmt.Errorf("semantic: parse template %s: %w", l, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, struct{ Input string }{Input: input}); err != nil {
		return "", fmt.Errorf("semantic: execute template %s: %w", l, err)
	}
	return buf.String(), nil
}

// renderFixPrompt builds the retry prompt sent when the validator rejects
// the rewriter's first attempt. The list of dropped regions is embedded
// verbatim so the model can see what to put back.
//
// Purpose: Produce the cherry-pick retry prompt.
// Errors: Returns an error only if the template fails to compile.
func renderFixPrompt(l Level, input string, violations []Violation) (string, error) {
	base, err := renderPrompt(l, input)
	if err != nil {
		return "", err
	}
	if len(violations) == 0 {
		return base, nil
	}
	var b bytes.Buffer
	b.WriteString(base)
	b.WriteString("\n\n## Cherry-pick fix\n")
	b.WriteString("Your previous attempt dropped or modified the following regions. ")
	b.WriteString("Produce a corrected rewrite that keeps each one verbatim.\n\n")
	for _, v := range violations {
		fmt.Fprintf(&b, "- [%s] %s\n", v.Kind, v.Excerpt)
	}
	b.WriteString("\nOutput ONLY the corrected rewrite. No preamble.\n")
	return b.String(), nil
}
