// Package audit writes append-only JSONL audit entries for Claude Code sessions.
package audit

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/amargautam/pakka/internal/data"
	"github.com/amargautam/pakka/internal/hookevent"
)

// Entry is one line in the audit JSONL file.
type Entry struct {
	TS         string `json:"ts"`
	SessionID  string `json:"session_id"`
	Kind       string `json:"kind"`
	Tool       string `json:"tool,omitempty"`
	InputHash  string `json:"input_hash,omitempty"`
	OutputSize int    `json:"output_size"`
	Result     string `json:"result"`
	Reason     string `json:"reason,omitempty"`
}

// Run appends an audit entry for the given event and phase.
//
// Purpose: Record a structured audit line to ~/.pakka/audit/<session>.jsonl.
// Errors: Returns error on filesystem failures (mkdir, open, write).
func Run(event *hookevent.Event, phase string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	short := shortSID(event.SessionID)
	path := filepath.Join(home, ".pakka", "audit", short+".jsonl")

	_, statErr := os.Stat(path)
	isNew := os.IsNotExist(statErr)

	if isNew {
		if err := data.AppendJSONL(path, map[string]string{"schema": "pakka.audit.v1"}); err != nil {
			return err
		}
	}

	kind := "tool_use"
	if phase == "session-end" {
		kind = "status"
	}

	var inputHash string
	if len(event.ToolInput) > 0 {
		h := sha256.Sum256(event.ToolInput)
		inputHash = fmt.Sprintf("sha256:%x", h[:])
	}

	entry := Entry{
		TS:         time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:  event.SessionID,
		Kind:       kind,
		Tool:       event.ToolName,
		InputHash:  inputHash,
		OutputSize: len(event.ToolResponse),
		Result:     "ok",
	}

	return data.AppendJSONL(path, entry)
}

// RunBlock logs a guard block to the audit trail.
//
// Purpose: Record blocked tool invocations with kind="guard_block".
// Errors: Returns error on filesystem failures.
func RunBlock(event *hookevent.Event, reason string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	short := shortSID(event.SessionID)
	path := filepath.Join(home, ".pakka", "audit", short+".jsonl")

	var inputHash string
	if len(event.ToolInput) > 0 {
		h := sha256.Sum256(event.ToolInput)
		inputHash = fmt.Sprintf("sha256:%x", h[:])
	}

	entry := Entry{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: event.SessionID,
		Kind:      "guard_block",
		Tool:      event.ToolName,
		InputHash: inputHash,
		Result:    "blocked",
		Reason:    reason,
	}

	return data.AppendJSONL(path, entry)
}

// WriteNote appends a custom audit entry for non-tool events.
//
// Purpose: Log commit-gate decisions, skips, and other lifecycle events.
// Errors: Returns error on filesystem failures.
func WriteNote(sessionID, kind, note string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	short := shortSID(sessionID)
	path := filepath.Join(home, ".pakka", "audit", short+".jsonl")
	entry := Entry{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: sessionID,
		Kind:      kind,
		Result:    "ok",
		Reason:    note,
	}
	return data.AppendJSONL(path, entry)
}

func shortSID(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}
