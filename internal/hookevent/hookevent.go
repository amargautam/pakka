// Package hookevent parses Claude Code hook events received on stdin.
package hookevent

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Event represents a Claude Code hook event.
//
// Fields match Claude Code's hook stdin JSON (all snake_case):
//   session_id, hook_event_name, tool_name, tool_input, tool_response, tool_use_id,
//   transcript_path, cwd, permission_mode.
type Event struct {
	SessionID      string          `json:"session_id"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`
	ToolUseID      string          `json:"tool_use_id"`
	TranscriptPath string          `json:"transcript_path"`
	CWD            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
}

// Parse reads and parses a hook event from r.
//
// Purpose: Deserialize Claude Code hook stdin into a typed Event.
// Errors: Never returns an error; returns a fallback Event on empty/malformed input.
func Parse(r io.Reader) (*Event, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return &Event{SessionID: fallbackSID()}, nil
	}
	if len(data) == 0 {
		return &Event{SessionID: fallbackSID()}, nil
	}
	var e Event
	if err := json.Unmarshal(data, &e); err != nil {
		return &Event{SessionID: fallbackSID()}, nil
	}
	if e.SessionID == "" {
		e.SessionID = fallbackSID()
	}
	return &e, nil
}

func fallbackSID() string {
	return fmt.Sprintf("sess-%d", time.Now().Unix())
}
