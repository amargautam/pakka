package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/hookevent"
)

func TestRunCreatesAuditFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	event := &hookevent.Event{
		SessionID: "test1234session",
		ToolName:  "Read",
		ToolInput: json.RawMessage(`{"file_path":"/tmp/test.go"}`),
	}

	if err := Run(event, "tool-post"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmp, ".pakka", "audit", "test1234.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("audit file not created: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected >= 2 lines (schema + entry), got %d", len(lines))
	}

	if !strings.Contains(lines[0], "pakka.audit.v1") {
		t.Errorf("first line should be schema preamble, got %q", lines[0])
	}

	var entry Entry
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("failed to parse audit entry: %v", err)
	}
	if entry.SessionID != "test1234session" {
		t.Errorf("session_id = %q, want %q", entry.SessionID, "test1234session")
	}
	if entry.Tool != "Read" {
		t.Errorf("tool = %q, want %q", entry.Tool, "Read")
	}
	if entry.Kind != "tool_use" {
		t.Errorf("kind = %q, want %q", entry.Kind, "tool_use")
	}
	if entry.InputHash == "" {
		t.Error("input_hash should not be empty when tool_input provided")
	}
	if !strings.HasPrefix(entry.InputHash, "sha256:") {
		t.Errorf("input_hash should start with sha256:, got %q", entry.InputHash)
	}
}

func TestRunAppendsToExisting(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	event := &hookevent.Event{SessionID: "append12session", ToolName: "Write"}

	if err := Run(event, "tool-post"); err != nil {
		t.Fatal(err)
	}
	if err := Run(event, "tool-post"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmp, ".pakka", "audit", "append12.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// 1 schema + 2 entries = 3 lines
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestRunSessionEnd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	event := &hookevent.Event{SessionID: "sessend12xyz"}

	if err := Run(event, "session-end"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmp, ".pakka", "audit", "sessend1.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var entry Entry
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Kind != "status" {
		t.Errorf("kind = %q, want %q for session-end", entry.Kind, "status")
	}
}
