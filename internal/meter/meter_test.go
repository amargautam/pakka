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

	path := filepath.Join(tmp, ".pakka", "meter", "meter123session.jsonl")
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

	path := filepath.Join(tmp, ".pakka", "meter", "append12session.jsonl")
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

	path := filepath.Join(tmp, ".pakka", "meter", "savings1session.jsonl")
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

	if err := WriteSessionEnd("endtok01session", "/repo/y", 12345); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmp, ".pakka", "meter", "endtok01session.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("meter file not created: %v", err)
	}

	var entry Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.OutputTokens != 12345 {
		t.Errorf("output_tokens = %d, want 12345", entry.OutputTokens)
	}
	if entry.SessionID != "endtok01session" {
		t.Errorf("session_id = %q, want endtok01session", entry.SessionID)
	}
	if entry.Repo != "/repo/y" {
		t.Errorf("repo = %q, want /repo/y", entry.Repo)
	}
	// Round-trip: re-encode and decode, ensure OutputTokens is preserved.
	encoded, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"output_tokens":12345`) {
		t.Errorf("encoded entry missing output_tokens: %s", encoded)
	}
	var round Entry
	if err := json.Unmarshal(encoded, &round); err != nil {
		t.Fatal(err)
	}
	if round.OutputTokens != 12345 {
		t.Errorf("round-trip output_tokens = %d, want 12345", round.OutputTokens)
	}
}

// TestSessionFilesDoNotCollideOnSharedPrefix proves the fix for the 8-char
// shortSID truncation bug: two distinct sessions whose IDs share the first 8
// characters (the shape of real UUID session IDs) must write to separate meter
// files, not clobber/append into one and mis-attribute totals.
func TestSessionFilesDoNotCollideOnSharedPrefix(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	a := "abcdef12-1111-1111-1111-111111111111"
	b := "abcdef12-2222-2222-2222-222222222222"
	if err := WriteSavings(a, "/repo/x", 100); err != nil {
		t.Fatal(err)
	}
	if err := WriteSavings(b, "/repo/x", 200); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(tmp, ".pakka", "meter")
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		var names []string
		for _, f := range files {
			names = append(names, f.Name())
		}
		t.Fatalf("want 2 distinct meter files for prefix-sharing sessions, got %d: %v", len(files), names)
	}
}

func TestEstimateTokens(t *testing.T) {
	event := &hookevent.Event{
		ToolInput:    json.RawMessage(strings.Repeat("a", 100)),
		ToolResponse: json.RawMessage(strings.Repeat("b", 300)),
	}
	got := estimateTokens(event)
	if got != 114 { // round(400/3.5) = 114
		t.Errorf("estimateTokens = %d, want 114", got)
	}
}

func TestEstimateTokensEmpty(t *testing.T) {
	event := &hookevent.Event{}
	got := estimateTokens(event)
	if got != 0 {
		t.Errorf("estimateTokens on empty event = %d, want 0", got)
	}
}
