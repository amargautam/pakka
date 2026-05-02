package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// OutputRulesCmd implements the "output-rules" subcommand.
type OutputRulesCmd struct{}

func (c *OutputRulesCmd) Name() string { return "output-rules" }
func (c *OutputRulesCmd) Run(args []string) error {
	runOutputRules()
	return nil
}

// --- output-rules (Pass 4.1) ---

var (
	reFilterTableRow = regexp.MustCompile(`^\|\s*(lite|strict|ultra|super-ultra)\s*\|`)
	reFilterExample  = regexp.MustCompile(`^- (lite|strict|ultra|super-ultra): `)
)

// filterToLevel strips intensity table rows and example lines for levels other
// than activeLevel, reducing injected context noise for the active user level.
//
// Kept unconditionally: table header (| Level | Rules |), separator rows, all
// non-table / non-example prose lines.
// Stripped: any "| <other-level> |" table row and "- <other-level>: " example line.
func filterToLevel(text, activeLevel string) string {
	reTableRow := reFilterTableRow
	reExample := reFilterExample

	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if m := reTableRow.FindStringSubmatch(line); m != nil {
			if m[1] != activeLevel {
				continue
			}
		} else if m := reExample.FindStringSubmatch(line); m != nil {
			if m[1] != activeLevel {
				continue
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	// strings.Split always produces a trailing empty element; trim the extra newline.
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// outputCompressRulesetFallback is emitted when rules/output-compress.md is missing.
const outputCompressRulesetFallback = `PAKKA OUTPUT COMPRESSION ACTIVE — level: ultra

## Persistence
Active every response. No revert after many turns. No filler drift.
Still active if unsure. Off only: user says "pakka verbose" or "normal mode".
Default: ultra. Switch: /pakka:compress lite|strict|ultra|super-ultra

## Rules
Drop: articles (a/an/the), filler (just/really/basically/actually/simply),
pleasantries (sure/certainly/of course/happy to), hedging (I think/maybe/perhaps).
Fragments OK. Short synonyms (big not extensive, fix not "implement a solution for").
Technical terms exact. Code blocks unchanged. Errors quoted exact.
Pattern: [thing] [action] [reason]. [next step].

Not: "Sure! I'd be happy to help you with that. The issue you're experiencing is..."
Yes: "Bug in auth middleware. Token expiry uses < not <=. Fix:"

## Intensity
| Level | Rules |
|-------|-------|
| lite | No filler/hedging. Keep articles + full sentences. Professional tight. |
| strict | Drop articles, fragments OK, short synonyms. |
| ultra | Default. Abbreviate (DB/auth/config/req/res/fn/impl), strip conjunctions, arrows for causality (X -> Y), one word when one word enough. |

## Auto-Clarity
Drop compression for: security warnings, irreversible action confirmations,
multi-step sequences where fragments risk misread, user asks to clarify.
Resume after clear part done.

## Boundaries
Code/commits/PRs/error messages: write normal. Never compress code output.
`

// runOutputRules reads the output compression ruleset and emits it to stdout.
// Used by SessionStart hook to inject output compression rules into context.
//
// Purpose: Provide output compression rules as additional session context.
// Errors: Falls back to hardcoded ruleset if file not found.
func runOutputRules() {
	if !isOutputEnabled() {
		return
	}

	level := loadOutputLevel()

	// Try to read ruleset from plugin root
	root := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if root == "" {
		root = pluginRoot()
	}
	rulesetPath := filepath.Join(root, "rules", "output-compress.md")

	content, err := os.ReadFile(rulesetPath)
	if err != nil {
		// Fallback to hardcoded ruleset
		content = []byte(outputCompressRulesetFallback)
	}

	// Replace the default level marker in the ruleset header with the user's
	// configured level. The ruleset ships with `level: ultra` as the brand
	// default; we accept the legacy `level: strict` form too so users with
	// older rules/output-compress.md files continue to work.
	out := string(content)
	if strings.Contains(out, "level: ultra") {
		out = strings.Replace(out, "level: ultra", "level: "+level, 1)
	} else {
		out = strings.Replace(out, "level: strict", "level: "+level, 1)
	}
	out = filterToLevel(out, level)
	fmt.Fprint(os.Stdout, out)

	// Pass 4.2: append skill auto-invocation mandates.
	// skill-invoke.md is emitted as additional SessionStart context so Claude
	// knows which skills to invoke automatically without being explicitly asked.
	// Silently skip if the file is missing — no fallback needed; skills still
	// appear in <available-skills> and their description triggers still work.
	skillPath := filepath.Join(root, "rules", "skill-invoke.md")
	if skillContent, err := os.ReadFile(skillPath); err == nil {
		fmt.Fprint(os.Stdout, "\n")
		fmt.Fprint(os.Stdout, string(skillContent))
	}
}
