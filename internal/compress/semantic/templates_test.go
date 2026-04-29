package semantic

import (
	"strings"
	"testing"
)

// Each level template must render with {{.Input}} substitution and contain
// the explicit "preserve verbatim" constraint block. This is the prompt
// contract the validator enforces — if the template stops carrying it, the
// gate reduces to retry-luck.
func TestTemplates_RenderAndContainPreserveBlock(t *testing.T) {
	probe := "ALPHA-BETA-GAMMA-INPUT-MARKER"
	for _, level := range AllLevels() {
		t.Run(string(level), func(t *testing.T) {
			out, err := renderPrompt(level, probe)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(out, probe) {
				t.Errorf("level=%s: rendered prompt missing input substitution", level)
			}
			// Constraint phrasings every template must carry.
			for _, must := range []string{
				"Preserve verbatim",
				"Fenced code blocks",
				"URLs",
				"File paths",
				"Dates",
				"Version strings",
				"Environment variables",
				"TODO",
			} {
				if !strings.Contains(out, must) {
					t.Errorf("level=%s: missing constraint %q in rendered prompt", level, must)
				}
			}
			// Level marker must appear in the prompt body so the model knows
			// which set of rules to apply.
			if !strings.Contains(out, "level="+string(level)) {
				t.Errorf("level=%s: prompt body must self-identify the level", level)
			}
		})
	}
}

// Banned vocabulary scrub: templates must not echo external project names
// (per memory/DECISIONS.md "no caveman / wenyan / grunt" rule).
func TestTemplates_NoExternalBranding(t *testing.T) {
	banned := []string{"caveman", "wenyan", "grunt"}
	for _, level := range AllLevels() {
		out, err := renderPrompt(level, "input")
		if err != nil {
			t.Fatalf("level=%s render: %v", level, err)
		}
		low := strings.ToLower(out)
		for _, b := range banned {
			if strings.Contains(low, b) {
				t.Errorf("level=%s: banned brand %q present in template", level, b)
			}
		}
	}
}

// Each level renders a DIFFERENT prompt body. If two levels emit the same
// template, the level switch is a no-op and tests downstream become theatre.
func TestTemplates_LevelsAreDistinct(t *testing.T) {
	rendered := map[Level]string{}
	for _, level := range AllLevels() {
		out, err := renderPrompt(level, "input")
		if err != nil {
			t.Fatalf("level=%s: render: %v", level, err)
		}
		rendered[level] = out
	}
	for a := range rendered {
		for b := range rendered {
			if a == b {
				continue
			}
			if rendered[a] == rendered[b] {
				t.Errorf("levels %q and %q render the same template body", a, b)
			}
		}
	}
}

// Fix prompt embeds the violation list so the model can put dropped regions
// back. Empty violations short-circuit to the base prompt.
func TestTemplates_FixPromptEmbedsViolations(t *testing.T) {
	in := "input text"
	v := []Violation{
		{Kind: KindURLChanged, Excerpt: "https://pakka.dev/docs"},
		{Kind: KindCodeBlockModified, Excerpt: "```go\nfmt.Println()\n```"},
	}
	out, err := renderFixPrompt(LevelStrict, in, v)
	if err != nil {
		t.Fatalf("fix render: %v", err)
	}
	for _, must := range []string{
		"Cherry-pick fix",
		"https://pakka.dev/docs",
		"fmt.Println()",
		string(KindURLChanged),
		string(KindCodeBlockModified),
	} {
		if !strings.Contains(out, must) {
			t.Errorf("fix prompt missing %q\nfull:\n%s", must, out)
		}
	}

	// No violations → base prompt only (no Cherry-pick section).
	plain, err := renderFixPrompt(LevelStrict, in, nil)
	if err != nil {
		t.Fatalf("fix render plain: %v", err)
	}
	if strings.Contains(plain, "Cherry-pick fix") {
		t.Errorf("empty violations should skip cherry-pick section")
	}
}
