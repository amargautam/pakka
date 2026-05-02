package data_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/data"
)

func TestAppendJSONL_CreatesFileAndDir(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "out.jsonl")
	entry := map[string]string{"key": "val"}
	if err := data.AppendJSONL(path, entry); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"val"`) {
		t.Errorf("file missing entry: %s", got)
	}
}

func TestAppendJSONL_AppendsMultipleLines(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "out.jsonl")
	for i := 0; i < 3; i++ {
		if err := data.AppendJSONL(path, map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != 3 {
		t.Errorf("want 3 lines, got %d: %s", len(lines), got)
	}
}

func TestReadLines_ReturnsNonEmptyLines(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.jsonl")
	content := "{\"a\":1}\n\n{\"b\":2}\n"
	os.WriteFile(path, []byte(content), 0644)

	lines, err := data.ReadLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Errorf("want 2 lines, got %d: %v", len(lines), lines)
	}
}

func TestReadLines_MissingFileReturnsError(t *testing.T) {
	_, err := data.ReadLines("/nonexistent/path.jsonl")
	if err == nil {
		t.Error("want error for missing file")
	}
}
