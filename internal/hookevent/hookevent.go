// Package hookevent defines the Claude Code hook event type.
//
// Parse logic has been inlined into each caller in cmd/pakka-core/main.go
// to surface errors appropriately: hard-fail for guard/commit-gate/stack-gate,
// silent fallback for meter/audit/statusline/compress.
package hookevent

import "encoding/json"

// Event represents a Claude Code hook event.
//
// Fields match Claude Code's hook stdin JSON (all snake_case):
//
//	session_id, hook_event_name, tool_name, tool_input, tool_response, tool_use_id,
//	transcript_path, cwd, permission_mode.
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
