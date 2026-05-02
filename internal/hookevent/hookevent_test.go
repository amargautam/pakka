package hookevent

import (
	"encoding/json"
	"testing"
)

// TestEventJSONRoundTrip verifies that the Event struct correctly deserializes
// from the JSON shape Claude Code sends on stdin.
func TestEventJSONRoundTrip(t *testing.T) {
	input := `{"session_id":"abc123","hook_event_name":"Stop","tool_name":"Read","cwd":"/work/x"}`
	var e Event
	if err := json.Unmarshal([]byte(input), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
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
	if e.CWD != "/work/x" {
		t.Errorf("cwd = %q, want %q", e.CWD, "/work/x")
	}
}

// TestEventToolInputRawMessage verifies that tool_input is preserved as raw JSON.
func TestEventToolInputRawMessage(t *testing.T) {
	input := `{"session_id":"s1","tool_name":"Read","tool_input":{"file_path":"/tmp/x.go"}}`
	var e Event
	if err := json.Unmarshal([]byte(input), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(e.ToolInput) != `{"file_path":"/tmp/x.go"}` {
		t.Errorf("tool_input = %s, want {\"file_path\":\"/tmp/x.go\"}", string(e.ToolInput))
	}
}

// TestEventEmptyInputGivesZeroValue verifies zero-value semantics on empty JSON.
func TestEventEmptyInputGivesZeroValue(t *testing.T) {
	var e Event
	if err := json.Unmarshal([]byte("{}"), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.SessionID != "" {
		t.Errorf("empty JSON: session_id should be empty, got %q", e.SessionID)
	}
}

// TestEventVariesWithInput — behavioral guard: two different JSON inputs must
// produce two different Event values.
func TestEventVariesWithInput(t *testing.T) {
	var e1, e2 Event
	_ = json.Unmarshal([]byte(`{"session_id":"A","tool_name":"Read"}`), &e1)
	_ = json.Unmarshal([]byte(`{"session_id":"B","tool_name":"Bash"}`), &e2)
	if e1.SessionID == e2.SessionID {
		t.Errorf("SessionID must differ; both=%q", e1.SessionID)
	}
	if e1.ToolName == e2.ToolName {
		t.Errorf("ToolName must differ; both=%q", e1.ToolName)
	}
}
