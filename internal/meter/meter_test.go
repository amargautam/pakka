package meter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/hookevent"
)

func TestRunCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	event := &hookevent.Event{
		SessionID:    "meter123session",
		ToolName:     "Read",
		ToolInput:    json.RawMessage(`{"file_path":"/tmp/x.go"}`),
		ToolResponse: json.RawMessage(`"file contents here, about forty chars..."`),
	}

	if err := Run(event); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmp, ".pakka", "meter", "meter123.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("meter file not created: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatalf("failed to parse entry: %v", err)
	}
	if entry.SessionID != "meter123session" {
		t.Errorf("session_id = %q, want %q", entry.SessionID, "meter123session")
	}
	if entry.TokensUsed <= 0 {
		t.Errorf("tokens_used should be > 0, got %d", entry.TokensUsed)
	}
	if entry.TS == "" {
		t.Error("ts should not be empty")
	}
}

func TestRunAppendsMultiple(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	event := &hookevent.Event{
		SessionID: "append12session",
		ToolName:  "Write",
		ToolInput: json.RawMessage(`{"content":"hello"}`),
	}

	for i := 0; i < 3; i++ {
		if err := Run(event); err != nil {
			t.Fatal(err)
		}
	}

	path := filepath.Join(tmp, ".pakka", "meter", "append12.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestWriteSavings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := WriteSavings("savings1session", "/repo/x", 4200); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmp, ".pakka", "meter", "savings1.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("meter file not created: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.BytesSaved != 4200 {
		t.Errorf("bytes_saved = %d, want 4200", entry.BytesSaved)
	}
	if entry.TokensSavedEst != 1200 { // round(4200 / 3.5) = 1200
		t.Errorf("tokens_saved_est = %d, want 1200", entry.TokensSavedEst)
	}
	if entry.TokensUsed != 0 {
		t.Errorf("tokens_used = %d, want 0 for savings entry", entry.TokensUsed)
	}
	if entry.Repo != "/repo/x" {
		t.Errorf("repo = %q, want /repo/x", entry.Repo)
	}
}

func TestWriteOutputTokens(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := WriteOutputTokens("outtoks1session", "/repo/y", 1234); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmp, ".pakka", "meter", "outtoks1.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("meter file not created: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.OutputTokens != 1234 {
		t.Errorf("output_tokens = %d, want 1234", entry.OutputTokens)
	}
	if entry.SessionID != "outtoks1session" {
		t.Errorf("session_id = %q, want %q", entry.SessionID, "outtoks1session")
	}
	if entry.TS == "" {
		t.Error("ts should not be empty")
	}
	// Other counters should be zero for output-tokens-only entries.
	if entry.TokensUsed != 0 || entry.BytesSaved != 0 || entry.TokensSavedEst != 0 {
		t.Errorf("expected only OutputTokens populated, got %+v", entry)
	}
}

func TestEstimateTokens(t *testing.T) {
	event := &hookevent.Event{
		ToolInput:    json.RawMessage(strings.Repeat("a", 100)),
		ToolResponse: json.RawMessage(strings.Repeat("b", 300)),
	}
	got := estimateTokens(event)
	if got != 100 { // (100+300)/4
		t.Errorf("estimateTokens = %d, want 100", got)
	}
}

func TestEstimateTokensEmpty(t *testing.T) {
	event := &hookevent.Event{}
	got := estimateTokens(event)
	if got != 0 {
		t.Errorf("estimateTokens on empty event = %d, want 0", got)
	}
}
