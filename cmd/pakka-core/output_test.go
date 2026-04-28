package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/compress"
)

// --- output-rules tests ---

func TestOutputRules(t *testing.T) {
	tests := []struct {
		name        string
		filePresent bool
		enabled     bool
		level       string
		wantEmpty   bool
		wantLevel   string
	}{
		{"file present, strict", true, true, "strict", false, "level: strict"},
		{"file present, ultra", true, true, "ultra", false, "level: ultra"},
		{"file present, lite", true, true, "lite", false, "level: lite"},
		{"file missing, fallback", false, true, "strict", false, "level: strict"},
		{"output disabled", true, false, "strict", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.filePresent {
				rulesDir := filepath.Join(dir, "rules")
				_ = os.MkdirAll(rulesDir, 0755)
				content := "PAKKA OUTPUT COMPRESSION ACTIVE — level: strict\nTest rules.\n"
				_ = os.WriteFile(filepath.Join(rulesDir, "output-compress.md"), []byte(content), 0644)
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
func simulateOutputRules(pluginDir string, enabled bool, level string) string {
	if !enabled {
		return ""
	}
	rulesetPath := filepath.Join(pluginDir, "rules", "output-compress.md")
	content, err := os.ReadFile(rulesetPath)
	if err != nil {
		content = []byte(outputCompressRulesetFallback)
	}
	return strings.Replace(string(content), "level: strict", "level: "+level, 1)
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

// min is provided by Go 1.21+ builtin
