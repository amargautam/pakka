package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/statusline"
)

// TestParseStatusLineInput_EventAndNativeFromSameStdin — the status-line
// subcommand gets ONE stdin read; both the legacy hook-event fields and the
// CC 2.1 native fields must come from it, and the parsed values must track
// the payload.
func TestParseStatusLineInput_EventAndNativeFromSameStdin(t *testing.T) {
	payloadA := `{"session_id":"sessA","cwd":"/repo/A","transcript_path":"/tA.jsonl",
		"cost":{"total_cost_usd":0.5},
		"context_window":{"context_window_size":200000,"used_percentage":12,
			"current_usage":{"input_tokens":11000,"output_tokens":900,
				"cache_creation_input_tokens":6000,"cache_read_input_tokens":7000}}}`
	payloadB := `{"session_id":"sessB","cwd":"/repo/B","transcript_path":"/tB.jsonl",
		"context_window":{"context_window_size":200000,"used_percentage":70,
			"current_usage":{"input_tokens":120000,"output_tokens":4000,
				"cache_creation_input_tokens":10000,"cache_read_input_tokens":10000}}}`

	evA, natA := parseStatusLineInput(strings.NewReader(payloadA))
	evB, natB := parseStatusLineInput(strings.NewReader(payloadB))

	if evA.SessionID != "sessA" || evA.CWD != "/repo/A" {
		t.Errorf("event A misparsed: %+v", evA)
	}
	if evB.SessionID != "sessB" {
		t.Errorf("event B misparsed: %+v", evB)
	}
	if natA == nil || natA.ContextWindow == nil || natA.ContextWindow.CurrentUsage == nil {
		t.Fatalf("native A must parse from same stdin: %+v", natA)
	}
	if got := natA.ContextWindow.CurrentUsage.InputTokens; got != 11000 {
		t.Errorf("native A input_tokens = %d, want 11000", got)
	}
	if got := natB.ContextWindow.CurrentUsage.InputTokens; got != 120000 {
		t.Errorf("native B input_tokens = %d, want 120000", got)
	}
	if natA.ContextWindow.CurrentUsage.InputTokens == natB.ContextWindow.CurrentUsage.InputTokens {
		t.Error("native usage must vary with stdin payload")
	}
	if natA.Cost == nil || natA.Cost.TotalCostUSD != 0.5 {
		t.Errorf("native A cost = %+v, want 0.5", natA.Cost)
	}

	// Pre-2.1 stdin: event still parses, native context data is absent.
	evOld, natOld := parseStatusLineInput(strings.NewReader(`{"session_id":"old1","cwd":"/repo/C"}`))
	if evOld.SessionID != "old1" {
		t.Errorf("pre-2.1 event misparsed: %+v", evOld)
	}
	if natOld != nil && natOld.ContextWindow != nil {
		t.Errorf("pre-2.1 stdin must yield no native context data: %+v", natOld)
	}
}

func TestStatusLineCmdName(t *testing.T) {
	cmd := &StatusLineCmd{}
	if cmd.Name() != "status-line" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "status-line")
	}
}

func TestStatusLineCmdImplementsCommand(t *testing.T) {
	var _ Command = &StatusLineCmd{}
}

func TestCWDFromTranscriptDir(t *testing.T) {
	// Create a temp dir with a fake transcript containing a cwd field.
	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	content := `{"type":"attachment","cwd":"/Users/test/myproject"}` + "\n"
	if err := os.WriteFile(transcript, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := statusline.ReadCWDFromTranscriptPath(transcript)
	if got != "/Users/test/myproject" {
		t.Errorf("ReadCWDFromTranscriptPath = %q, want %q", got, "/Users/test/myproject")
	}
}

func TestCWDFromTranscriptDirEmpty(t *testing.T) {
	got := statusline.ReadCWDFromTranscriptPath("")
	if got != "" {
		t.Errorf("ReadCWDFromTranscriptPath(\"\") = %q, want \"\"", got)
	}
}
