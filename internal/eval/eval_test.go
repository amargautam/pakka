package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp creates a temporary file with the given content and returns its path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// validSkill is a minimal valid skill file for testing.
const validSkill = `---
name: test-skill
description: A test skill for unit tests
---

This is the body of the skill.

## Red Flags

- May produce false positives on edge cases
`

func TestRun(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		maxLayer  int
		passed    bool
		wantErr   string // substring in errors; empty means expect pass
		layerCnt  int    // expected layer count
	}{
		{
			name:     "valid skill file",
			content:  validSkill,
			maxLayer: 1,
			passed:   true,
			layerCnt: 1,
		},
		{
			name: "missing frontmatter",
			content: `# No frontmatter here

## Red Flags

- Something bad
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "missing frontmatter",
			layerCnt: 1,
		},
		{
			name: "missing Red Flags section",
			content: `---
name: test
description: test
---

Body text without red flags.
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "missing Red Flags section",
			layerCnt: 1,
		},
		{
			name: "banned word in body",
			content: `---
name: test
description: test
---

This skill will guarantee results.

## Red Flags

- False positives possible
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "banned word: guarantee",
			layerCnt: 1,
		},
		{
			name: "long line over 200 chars",
			content: `---
name: test
description: test
---

` + strings.Repeat("x", 201) + `

## Red Flags

- Something
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "line too long",
			layerCnt: 1,
		},
		{
			name: "long line inside code block passes",
			content: "---\nname: test\ndescription: test\n---\n\n```\n" + strings.Repeat("x", 250) + "\n```\n\n## Red Flags\n\n- Something\n",
			maxLayer: 1,
			passed:   true,
			layerCnt: 1,
		},
		{
			name: "URL line over 200 chars passes",
			content: `---
name: test
description: test
---

See https://example.com/` + strings.Repeat("a", 200) + `

## Red Flags

- Something
`,
			maxLayer: 1,
			passed:   true,
			layerCnt: 1,
		},
		{
			name:     "empty file",
			content:  "",
			maxLayer: 1,
			passed:   false,
			wantErr:  "empty file",
			layerCnt: 1,
		},
		{
			name:     "maxLayer=1 only runs layer 1",
			content:  validSkill,
			maxLayer: 1,
			passed:   true,
			layerCnt: 1,
		},
		{
			name:     "maxLayer=3 runs all layers",
			content:  validSkill,
			maxLayer: 3,
			passed:   true,
			layerCnt: 3,
		},
		{
			name:     "maxLayer=0 defaults to all layers",
			content:  validSkill,
			maxLayer: 0,
			passed:   true,
			layerCnt: 3,
		},
		{
			name: "multiple banned words",
			content: `---
name: test
description: test
---

This is seamless and revolutionary software.

## Red Flags

- Something
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "banned word: revolutionary",
			layerCnt: 1,
		},
		{
			name: "banned word case insensitive",
			content: `---
name: test
description: test
---

This will GUARANTEE success and is DELIGHTFUL.

## Red Flags

- Something
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "banned word: guarantee",
			layerCnt: 1,
		},
		{
			name: "Red Flags with ### heading",
			content: `---
name: test
description: test
---

Body text.

### Red Flags

- A red flag item
`,
			maxLayer: 1,
			passed:   true,
			layerCnt: 1,
		},
		{
			name: "Red Flags header but no bullets",
			content: `---
name: test
description: test
---

## Red Flags

Some text but no bullets.

## Next Section
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "missing Red Flags section",
			layerCnt: 1,
		},
		{
			name: "frontmatter missing name field",
			content: `---
description: test
---

Body.

## Red Flags

- Something
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "frontmatter missing name field",
			layerCnt: 1,
		},
		{
			name: "frontmatter missing description field",
			content: `---
name: test
---

Body.

## Red Flags

- Something
`,
			maxLayer: 1,
			passed:   false,
			wantErr:  "frontmatter missing description field",
			layerCnt: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, "SKILL.md", tt.content)
			result := Run([]string{path}, tt.maxLayer)

			if result.Passed != tt.passed {
				t.Errorf("Passed = %v, want %v; errors: %v",
					result.Passed, tt.passed, collectErrors(result))
			}

			if len(result.Layers) != tt.layerCnt {
				t.Errorf("layer count = %d, want %d", len(result.Layers), tt.layerCnt)
			}

			if tt.wantErr != "" {
				found := false
				for _, lr := range result.Layers {
					for _, e := range lr.Errors {
						if strings.Contains(e, tt.wantErr) {
							found = true
						}
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v",
						tt.wantErr, collectErrors(result))
				}
			}
		})
	}
}

func TestRunNonexistentFile(t *testing.T) {
	result := Run([]string{"/nonexistent/file.md"}, 1)
	if result.Passed {
		t.Error("expected failure for nonexistent file")
	}
	if len(result.Layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(result.Layers))
	}
	if !strings.Contains(result.Layers[0].Errors[0], "cannot read file") {
		t.Errorf("expected 'cannot read file' error, got: %v", result.Layers[0].Errors)
	}
}

func TestRunMultipleTargets(t *testing.T) {
	good := writeTemp(t, "good.md", validSkill)
	bad := writeTemp(t, "bad.md", "")

	result := Run([]string{good, bad}, 1)
	if result.Passed {
		t.Error("expected overall failure when one target fails")
	}
	if len(result.Layers) != 2 {
		t.Errorf("expected 2 layers, got %d", len(result.Layers))
	}
}

func TestLayer2Deferred(t *testing.T) {
	path := writeTemp(t, "SKILL.md", validSkill)
	result := Run([]string{path}, 2)

	var found bool
	for _, lr := range result.Layers {
		if lr.Layer == 2 {
			found = true
			if !lr.Passed {
				t.Error("layer 2 should pass (deferred)")
			}
			if !strings.Contains(lr.Details, "deferred to skill wrapper") {
				t.Errorf("unexpected details: %s", lr.Details)
			}
		}
	}
	if !found {
		t.Error("layer 2 result not found")
	}
}

func TestLayer3Deferred(t *testing.T) {
	path := writeTemp(t, "SKILL.md", validSkill)
	result := Run([]string{path}, 3)

	var found bool
	for _, lr := range result.Layers {
		if lr.Layer == 3 {
			found = true
			if !lr.Passed {
				t.Error("layer 3 should pass (deferred)")
			}
			if !strings.Contains(lr.Details, "headless claude -p") {
				t.Errorf("unexpected details: %s", lr.Details)
			}
		}
	}
	if !found {
		t.Error("layer 3 result not found")
	}
}

// collectErrors gathers all error strings from all layers for diagnostic output.
func collectErrors(r *Result) []string {
	var all []string
	for _, lr := range r.Layers {
		all = append(all, lr.Errors...)
	}
	return all
}

// --- Path-scoped schema tests (commands vs. skills/agents) ---
//
// These tests prove the layer-1 frontmatter schema VARIES with the target
// path, not just that Run() returns. Per memory: feedback_measurement_first.md.
//
// commandFile:   description present, no name field, has Red Flags.
// skillFile:     description present, no name field, has Red Flags. Same body.
// Same content, different parent dir → different verdicts.

// writeAtPath writes content to dir/relPath (creating parent dirs) and
// returns the absolute path. Used to control which dir the file appears in
// (skills/, agents/, commands/) so classifyTarget picks the right schema.
func writeAtPath(t *testing.T, relPath, content string) string {
	t.Helper()
	dir := t.TempDir()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

// commandWithoutName mirrors a real Claude Code command file: no name field,
// description present, Red Flags section present. Should pass under the
// command schema; should fail under the skill/agent schema.
const commandWithoutName = `---
description: A test command for unit tests
---

Body of the command.

## Red Flags

- A failure mode worth flagging.
`

func TestLayer1_commandSchema_skipsNameField(t *testing.T) {
	// File under commands/foo.md with no name: field — must PASS.
	path := writeAtPath(t, "commands/foo.md", commandWithoutName)
	result := Run([]string{path}, 1)

	if !result.Passed {
		t.Fatalf("expected pass for command without name field, got errors: %v", collectErrors(result))
	}
}

func TestLayer1_commandSchema_requiresDescription(t *testing.T) {
	// Command file missing description must FAIL with the right error.
	const noDesc = `---
argument-hint: "[args]"
---

Body.

## Red Flags

- Something.
`
	path := writeAtPath(t, "commands/bar.md", noDesc)
	result := Run([]string{path}, 1)

	if result.Passed {
		t.Fatal("expected fail for command missing description, got pass")
	}
	var found bool
	for _, lr := range result.Layers {
		for _, e := range lr.Errors {
			if strings.Contains(e, "missing description field") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected 'missing description field' error, got: %v", collectErrors(result))
	}
}

func TestLayer1_skillSchema_stillRequiresName(t *testing.T) {
	// Same file shape as commandWithoutName (no name field) — but placed
	// under skills/foo/SKILL.md, the strict schema must still flag it.
	// This is the path-scoping assertion: VARIES with target path.
	skillPath := writeAtPath(t, "skills/foo/SKILL.md", commandWithoutName)
	skillResult := Run([]string{skillPath}, 1)

	if skillResult.Passed {
		t.Fatal("expected fail for skill missing name field, got pass — schema not path-scoped")
	}
	var foundName bool
	for _, lr := range skillResult.Layers {
		for _, e := range lr.Errors {
			if strings.Contains(e, "missing name field") {
				foundName = true
			}
		}
	}
	if !foundName {
		t.Errorf("expected 'missing name field' error for skill, got: %v", collectErrors(skillResult))
	}

	// Behavior assertion — same content, different path → different verdict.
	cmdPath := writeAtPath(t, "commands/foo.md", commandWithoutName)
	cmdResult := Run([]string{cmdPath}, 1)
	if !cmdResult.Passed {
		t.Fatalf("control: command path should pass with same content, got errors: %v", collectErrors(cmdResult))
	}
	if skillResult.Passed == cmdResult.Passed {
		t.Fatal("schema did not vary with path: skill and command verdicts identical")
	}

	// Also verify agents/ keeps the strict schema.
	agentPath := writeAtPath(t, "agents/foo.md", commandWithoutName)
	agentResult := Run([]string{agentPath}, 1)
	if agentResult.Passed {
		t.Fatal("expected fail for agent missing name field, got pass — agents must keep strict schema")
	}
}
