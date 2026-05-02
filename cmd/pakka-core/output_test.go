package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/compress"
	"github.com/amargautam/pakka/internal/hookevent"
)

// --- output-rules tests ---

// TestOutputRules covers both the legacy ruleset shape (`level: strict`
// header) and the Pass-4.4 shape (`level: ultra` header). Pass 4.4 flipped
// the brand default from strict to ultra; runOutputRules must replace
// whichever marker is present so older rules/output-compress.md files keep
// working through a plugin upgrade.
func TestOutputRules(t *testing.T) {
	tests := []struct {
		name        string
		filePresent bool
		// fileBody is the raw ruleset committed to the temp plugin root.
		// Empty string means "use the legacy `level: strict` body".
		fileBody  string
		enabled   bool
		level     string
		wantEmpty bool
		wantLevel string
	}{
		{"legacy file (level: strict header), select strict", true, "", true, "strict", false, "level: strict"},
		{"legacy file, select ultra", true, "", true, "ultra", false, "level: ultra"},
		{"legacy file, select lite", true, "", true, "lite", false, "level: lite"},
		{"new file (level: ultra header), select strict", true, "PAKKA OUTPUT COMPRESSION ACTIVE — level: ultra\nTest rules.\n", true, "strict", false, "level: strict"},
		{"new file, select ultra", true, "PAKKA OUTPUT COMPRESSION ACTIVE — level: ultra\nTest rules.\n", true, "ultra", false, "level: ultra"},
		{"file missing, fallback emits ultra default", false, "", true, "ultra", false, "level: ultra"},
		{"file missing, fallback honours explicit strict", false, "", true, "strict", false, "level: strict"},
		{"output disabled", true, "", false, "strict", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.filePresent {
				rulesDir := filepath.Join(dir, "rules")
				_ = os.MkdirAll(rulesDir, 0755)
				body := tt.fileBody
				if body == "" {
					body = "PAKKA OUTPUT COMPRESSION ACTIVE — level: strict\nTest rules.\n"
				}
				_ = os.WriteFile(filepath.Join(rulesDir, "output-compress.md"), []byte(body), 0644)
			}

			result := simulateOutputRules(dir, tt.enabled, tt.level)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected empty output, got %q", result[:min(len(result), 80)])
				}
				return
			}

			if result == "" {
				t.Fatal("expected output, got empty")
			}
			if !strings.Contains(result, "PAKKA OUTPUT COMPRESSION ACTIVE") {
				t.Errorf("missing header in output")
			}
			if !strings.Contains(result, tt.wantLevel) {
				t.Errorf("expected %q in output, got %q", tt.wantLevel, result[:min(len(result), 80)])
			}
		})
	}
}

// simulateOutputRules mirrors runOutputRules logic for testing.
//
// Replacement logic must match production: the ruleset may carry either the
// legacy `level: strict` marker or the Pass-4.4 `level: ultra` marker. We
// substitute the configured level into whichever one is present.
func simulateOutputRules(pluginDir string, enabled bool, level string) string {
	if !enabled {
		return ""
	}
	rulesetPath := filepath.Join(pluginDir, "rules", "output-compress.md")
	content, err := os.ReadFile(rulesetPath)
	if err != nil {
		content = []byte(outputCompressRulesetFallback)
	}
	out := string(content)
	if strings.Contains(out, "level: ultra") {
		out = strings.Replace(out, "level: ultra", "level: "+level, 1)
	} else {
		out = strings.Replace(out, "level: strict", "level: "+level, 1)
	}
	return out
}

// --- output-reinforce tests ---

func TestOutputReinforce(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		level     string
		wantEmpty bool
	}{
		{"strict enabled", true, "strict", false},
		{"lite enabled", true, "lite", false},
		{"ultra enabled", true, "ultra", false},
		{"disabled", false, "strict", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := simulateOutputReinforce(tt.enabled, tt.level)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected empty, got %q", result)
				}
				return
			}

			if result == "" {
				t.Fatal("expected JSON output, got empty")
			}

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			hso, ok := parsed["hookSpecificOutput"].(map[string]interface{})
			if !ok {
				t.Fatal("missing hookSpecificOutput")
			}
			if hso["hookEventName"] != "UserPromptSubmit" {
				t.Errorf("wrong hookEventName: %v", hso["hookEventName"])
			}
			ctx := hso["additionalContext"].(string)
			if !strings.Contains(ctx, "PAKKA OUTPUT COMPRESSION ACTIVE") {
				t.Errorf("missing active marker in %q", ctx)
			}
			if !strings.Contains(ctx, "("+tt.level+")") {
				t.Errorf("expected (%s) in %q", tt.level, ctx)
			}
		})
	}
}

func simulateOutputReinforce(enabled bool, level string) string {
	if !enabled {
		return ""
	}
	reinforce := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": fmt.Sprintf("PAKKA OUTPUT COMPRESSION ACTIVE (%s). Drop articles/filler/pleasantries/hedging. Fragments OK. Code/commits/security: write normal.", level),
		},
	}
	data, _ := json.Marshal(reinforce)
	return string(data)
}

// --- compress --phase=tool-result tests ---

func TestToolResult(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		size      int // number of lines
		maxBytes  int
		isError   bool
		wantEmpty bool
	}{
		{"under threshold", "Read", 5, 10240, false, true},
		{"over threshold, Read", "Read", 200, 1024, false, false},
		{"over threshold, Bash", "Bash", 200, 1024, false, false},
		{"over threshold, Grep", "Grep", 200, 1024, false, false},
		{"error exit passthrough", "Bash", 200, 1024, true, true},
		{"Edit passthrough", "Edit", 200, 100, false, true},
		{"Write passthrough", "Write", 200, 100, false, true},
		{"not enough lines to truncate", "Read", 95, 100, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build response with specified number of lines
			var lines []string
			for i := 0; i < tt.size; i++ {
				lines = append(lines, fmt.Sprintf("line %d: %s", i, strings.Repeat("x", 50)))
			}
			response := strings.Join(lines, "\n")

			result := simulateToolResult(tt.tool, response, tt.maxBytes, 80, 20, tt.isError)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected passthrough (empty), got %d bytes", len(result))
				}
				return
			}

			if result == "" {
				t.Fatal("expected truncated output, got empty")
			}

			// Verify JSON structure
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			hso := parsed["hookSpecificOutput"].(map[string]interface{})
			toolResult := hso["toolResult"].(string)

			if !strings.Contains(toolResult, "[pakka: truncated") {
				t.Error("missing truncation notice")
			}
			if !strings.Contains(toolResult, "Use offset/limit") {
				t.Error("missing offset/limit hint")
			}

			// Verify truncated is smaller than original
			if len(toolResult) >= len(response) {
				t.Errorf("truncated (%d) should be smaller than original (%d)", len(toolResult), len(response))
			}
		})
	}
}

func simulateToolResult(toolName, response string, maxBytes, headLines, tailLines int, isError bool) string {
	if toolName == "Edit" || toolName == "Write" {
		return ""
	}
	if isError {
		return ""
	}
	if response == "" || len(response) <= maxBytes {
		return ""
	}

	lines := strings.Split(response, "\n")
	totalLines := len(lines)
	if totalLines <= headLines+tailLines {
		return ""
	}

	truncatedLines := totalLines - headLines - tailLines

	var b strings.Builder
	for i := 0; i < headLines && i < totalLines; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf("\n[pakka: truncated %d lines, %s. Use offset/limit to read specific ranges.]\n\n",
		truncatedLines, compress.FmtSize(len(response))))
	for i := totalLines - tailLines; i < totalLines; i++ {
		b.WriteString(lines[i])
		if i < totalLines-1 {
			b.WriteByte('\n')
		}
	}

	out := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName": "PostToolUse",
			"toolResult":    b.String(),
		},
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// --- compress --phase=subagent-return tests ---

func TestSubagentReturn(t *testing.T) {
	tests := []struct {
		name      string
		size      int
		enabled   bool
		wantEmpty bool
	}{
		{"small passthrough", 512, true, true},
		{"exactly 1KB passthrough", 1024, true, true},
		{"large compressed", 5000, true, false},
		{"disabled passthrough", 5000, false, true},
		{"empty passthrough", 0, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build input with compressible content
			var input string
			if tt.size > 0 {
				var sb strings.Builder
				for sb.Len() < tt.size {
					sb.WriteString("This is a really very basically simple line of text.\n\n\n")
				}
				input = sb.String()[:tt.size]
			}

			result := simulateSubagentReturn(input, tt.enabled)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected passthrough (empty), got %d bytes", len(result))
				}
				return
			}

			if result == "" {
				t.Fatal("expected compressed output, got empty")
			}
			if len(result) >= len(input) {
				t.Errorf("compressed (%d) should be smaller than input (%d)", len(result), len(input))
			}
		})
	}
}

func simulateSubagentReturn(input string, enabled bool) string {
	if !enabled || input == "" || len(input) <= 1024 {
		return ""
	}
	m := compress.ParseMode("strict")
	result := compress.Run(input, m)
	if result.CompressedSize >= result.OriginalSize {
		return ""
	}
	return result.Output
}

// --- filterToLevel tests ---

func TestFilterToLevel(t *testing.T) {
	const input = `PAKKA COMPRESSION ACTIVE — level: ultra

## Intensity
| Level | Rules |
|-------|-------|
| lite | No filler/hedging. Keep articles + full sentences. |
| strict | Drop articles, fragments OK, short synonyms. |
| ultra | Default. Abbreviate, strip conjunctions. |
| super-ultra | Maximum density. One token where one suffices. |

## Examples

Some prose line kept regardless.

- lite: "Keep articles."
- strict: "Drop articles."
- ultra: "Abbrev."
- super-ultra: "Max density."

## Boundaries
Code/commits unchanged.
`

	tests := []struct {
		level          string
		wantTableRow   string
		wantExLine     string
		noTableRows    []string
		noExLines      []string
		preservedLines []string
	}{
		{
			level:        "lite",
			wantTableRow: "| lite | No filler/hedging. Keep articles + full sentences. |",
			wantExLine:   `- lite: "Keep articles."`,
			noTableRows:  []string{"| strict |", "| ultra |", "| super-ultra |"},
			noExLines:    []string{`- strict:`, `- ultra:`, `- super-ultra:`},
			preservedLines: []string{
				"## Intensity",
				"| Level | Rules |",
				"|-------|-------|",
				"Some prose line kept regardless.",
				"## Examples",
				"## Boundaries",
				"Code/commits unchanged.",
			},
		},
		{
			level:        "strict",
			wantTableRow: "| strict | Drop articles, fragments OK, short synonyms. |",
			wantExLine:   `- strict: "Drop articles."`,
			noTableRows:  []string{"| lite |", "| ultra |", "| super-ultra |"},
			noExLines:    []string{`- lite:`, `- ultra:`, `- super-ultra:`},
		},
		{
			level:        "ultra",
			wantTableRow: "| ultra | Default. Abbreviate, strip conjunctions. |",
			wantExLine:   `- ultra: "Abbrev."`,
			noTableRows:  []string{"| lite |", "| strict |", "| super-ultra |"},
			noExLines:    []string{`- lite:`, `- strict:`, `- super-ultra:`},
		},
		{
			level:        "super-ultra",
			wantTableRow: "| super-ultra | Maximum density. One token where one suffices. |",
			wantExLine:   `- super-ultra: "Max density."`,
			noTableRows:  []string{"| lite |", "| strict |", "| ultra |"},
			noExLines:    []string{`- lite:`, `- strict:`, `- ultra:`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			got := filterToLevel(input, tt.level)

			// Kept table row
			if !strings.Contains(got, tt.wantTableRow) {
				t.Errorf("missing table row for %q: %q", tt.level, tt.wantTableRow)
			}

			// Kept example line
			if !strings.Contains(got, tt.wantExLine) {
				t.Errorf("missing example line for %q: %q", tt.level, tt.wantExLine)
			}

			// Stripped other table rows
			for _, s := range tt.noTableRows {
				if strings.Contains(got, s) {
					t.Errorf("should have stripped table row %q but found it", s)
				}
			}

			// Stripped other example lines
			for _, s := range tt.noExLines {
				if strings.Contains(got, s) {
					t.Errorf("should have stripped example line %q but found it", s)
				}
			}

			// Header row, separator, and prose preserved
			for _, s := range tt.preservedLines {
				if !strings.Contains(got, s) {
					t.Errorf("expected preserved line %q missing from output", s)
				}
			}

			// Single trailing newline
			if !strings.HasSuffix(got, "\n") {
				t.Error("output does not end with newline")
			}
			if strings.HasSuffix(got, "\n\n") {
				t.Error("output has trailing double-newline, expected single")
			}
		})
	}
}

// min is provided by Go 1.21+ builtin

// TestToolResultWritesMeterEntry — behavioral guarantee that the truncation
// path appends a meter entry tagged with the supplied repo and a non-zero
// tokens_saved_est. Pass 4.1.1 diagnosis: the production meter for the
// pakka.dev repo carries only 5 tool-result entries with savings — not a
// missing write path, but the genuine consequence of most tool outputs
// being smaller than the 10KB threshold. This test guards the write-path
// itself: when truncation actually fires, savings ARE recorded.
func TestToolResultWritesMeterEntry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Build a tool response large enough to trip the 10KB default threshold.
	const repo = "/repo/X"
	var b strings.Builder
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&b, "line %d: %s\n", i, strings.Repeat("x", 60))
	}
	response := b.String()
	if len(response) < 10240 {
		t.Fatalf("test fixture too small: %d", len(response))
	}

	// Encode response as the JSON string Claude Code would deliver.
	respJSON, _ := json.Marshal(response)

	event := &hookevent.Event{
		SessionID:    "metertool",
		CWD:          repo, // RepoKey on a non-git path falls back to abs(cwd) = repo
		ToolName:     "Read",
		ToolResponse: respJSON,
	}

	// Redirect stdout so the truncated tool-result JSON does not pollute test output.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	runCompressToolResult(event)
	w.Close()
	os.Stdout = origStdout
	_, _ = io.Copy(io.Discard, r)

	// Expect a meter file at ~/.pakka/meter/<short-sid>.jsonl
	path := filepath.Join(tmp, ".pakka", "meter", "metertoo.jsonl") // shortSID truncates to 8
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("meter file not written: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 meter entry, got %d (%q)", len(lines), data)
	}

	var entry struct {
		Repo           string `json:"repo"`
		BytesSaved     int64  `json:"bytes_saved"`
		TokensSavedEst int64  `json:"tokens_saved_est"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if entry.Repo != repo {
		t.Errorf("entry.Repo = %q, want %q", entry.Repo, repo)
	}
	if entry.TokensSavedEst <= 0 {
		t.Errorf("tokens_saved_est must be > 0 after truncation, got %d", entry.TokensSavedEst)
	}
	if entry.BytesSaved <= 0 {
		t.Errorf("bytes_saved must be > 0, got %d", entry.BytesSaved)
	}
}

