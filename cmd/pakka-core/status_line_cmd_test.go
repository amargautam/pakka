package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amargautam/pakka/internal/statusline"
)

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
