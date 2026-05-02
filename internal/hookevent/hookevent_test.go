package hookevent

import (
	"strings"
	"testing"
)

func TestParseValidEvent(t *testing.T) {
	input := `{"session_id":"abc123","hook_event_name":"Stop","tool_name":"Read"}`
	e, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if e.SessionID != "abc123" {
		t.Errorf("session_id = %q, want %q", e.SessionID, "abc123")
	}
	if e.HookEventName != "Stop" {
		t.Errorf("hook_event_name = %q, want %q", e.HookEventName, "Stop")
	}
	if e.ToolName != "Read" {
		t.Errorf("tool_name = %q, want %q", e.ToolName, "Read")
	}
}

func TestParseEmptyInput(t *testing.T) {
	e, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if e.SessionID == "" {
		t.Error("expected fallback session_id, got empty")
	}
	if !strings.HasPrefix(e.SessionID, "sess-") {
		t.Errorf("fallback session_id should start with sess-, got %q", e.SessionID)
	}
}

func TestParseMalformedJSON(t *testing.T) {
	e, err := Parse(strings.NewReader("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	if e.SessionID == "" {
		t.Error("expected fallback session_id, got empty")
	}
}

func TestParseMissingSessionID(t *testing.T) {
	e, err := Parse(strings.NewReader(`{"hook_event_name":"Stop"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(e.SessionID, "sess-") {
		t.Errorf("missing session_id should use fallback, got %q", e.SessionID)
	}
}

func TestParseWithToolInput(t *testing.T) {
	input := `{"session_id":"s1","tool_name":"Read","tool_input":{"file_path":"/tmp/x.go"}}`
	e, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(e.ToolInput) != `{"file_path":"/tmp/x.go"}` {
		t.Errorf("tool_input = %s, want {\"file_path\":\"/tmp/x.go\"}", string(e.ToolInput))
	}
}
