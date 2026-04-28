package stackgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/amargautam/pakka/internal/hookevent"
)

func TestNoStackJSON(t *testing.T) {
	dir := t.TempDir()
	cfg := LoadConfig(dir)
	if cfg != nil {
		t.Fatalf("expected nil config for missing stack.json, got %+v", cfg)
	}

	// Run with nil config should pass
	event := &hookevent.Event{
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"main.go"}`),
		CWD:       dir,
	}
	r := Run(event, cfg)
	if !r.Passed {
		t.Fatalf("expected pass with nil config, got fail: %s", r.Output)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	pakkaDir := filepath.Join(dir, ".pakka")
	os.MkdirAll(pakkaDir, 0755)
	os.WriteFile(filepath.Join(pakkaDir, "stack.json"), []byte(`{
		"test_command": "go test ./...",
		"lint_command": "go vet ./...",
		"format_command": ""
	}`), 0644)

	cfg := LoadConfig(dir)
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.LintCommand != "go vet ./..." {
		t.Errorf("LintCommand: got %q, want %q", cfg.LintCommand, "go vet ./...")
	}
	if cfg.TestCommand != "go test ./..." {
		t.Errorf("TestCommand: got %q, want %q", cfg.TestCommand, "go test ./...")
	}
}

func TestLintCommandSucceeds(t *testing.T) {
	cfg := &Config{LintCommand: "true"} // "true" always exits 0
	event := &hookevent.Event{
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"main.go"}`),
	}
	r := Run(event, cfg)
	if !r.Passed {
		t.Fatalf("expected pass, got fail: %s", r.Output)
	}
}

func TestLintCommandFails(t *testing.T) {
	cfg := &Config{LintCommand: "echo 'lint error' >&2; exit 1"}
	event := &hookevent.Event{
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"main.go"}`),
	}
	r := Run(event, cfg)
	if r.Passed {
		t.Fatal("expected fail, got pass")
	}
	if r.Output == "" {
		t.Error("expected non-empty output on failure")
	}
}

func TestEmptyLintCommand(t *testing.T) {
	cfg := &Config{LintCommand: ""}
	event := &hookevent.Event{
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"main.go"}`),
	}
	r := Run(event, cfg)
	if !r.Passed {
		t.Fatalf("expected pass with empty lint command, got fail: %s", r.Output)
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main_test.go", true},
		{"main.go", false},
		{"app.test.ts", true},
		{"app.test.js", true},
		{"app.test.tsx", true},
		{"app.test.jsx", true},
		{"app.spec.ts", true},
		{"app.spec.js", true},
		{"test_main.py", true},
		{"main_test.py", true},
		{"main.py", false},
		{"README.md", false},
		{"/full/path/to/main_test.go", true},
		{"/full/path/to/main.go", false},
	}
	for _, tt := range tests {
		got := isTestFile(tt.path)
		if got != tt.want {
			t.Errorf("isTestFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestTestFileAlsoRunsTestCommand(t *testing.T) {
	cfg := &Config{
		LintCommand: "true",
		TestCommand: "echo 'test fail' >&2; exit 1",
	}
	event := &hookevent.Event{
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"main_test.go"}`),
	}
	r := Run(event, cfg)
	if r.Passed {
		t.Fatal("expected fail from test command on test file, got pass")
	}
}

func TestNonTestFileSkipsTestCommand(t *testing.T) {
	cfg := &Config{
		LintCommand: "true",
		TestCommand: "exit 1", // would fail if run
	}
	event := &hookevent.Event{
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"main.go"}`),
	}
	r := Run(event, cfg)
	if !r.Passed {
		t.Fatal("expected pass — test command should not run on non-test file")
	}
}
