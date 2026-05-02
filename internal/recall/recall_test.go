package recall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeDB returns a temp db path and cleanup func.
func makeDB(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "recall.db")
	return dbPath, func() {} // TempDir cleans up automatically.
}

// makeAuditFile writes a JSONL file with the given lines and returns its path.
func makeAuditFile(t *testing.T, dir string, lines []map[string]interface{}) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test-session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// TestIdempotency: indexing the same file twice must not grow the row count.
func TestIdempotency(t *testing.T) {
	dbPath, cleanup := makeDB(t)
	defer cleanup()

	auditDir := t.TempDir()
	lines := []map[string]interface{}{
		{"session_id": "abc123", "ts": "2025-01-01T00:00:00Z", "kind": "tool_use", "file_path": "/src/main.go", "content": "edit main file"},
		{"session_id": "abc123", "ts": "2025-01-01T00:01:00Z", "kind": "guard_block", "file_path": "/etc/passwd", "content": "blocked read"},
	}
	makeAuditFile(t, auditDir, lines)

	// First index pass.
	if err := Index(dbPath, auditDir); err != nil {
		t.Fatalf("first Index: %v", err)
	}

	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var count1 int
	if err := db.QueryRow(`SELECT count(*) FROM audit_fts`).Scan(&count1); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// Second index pass — must be idempotent.
	if err := Index(dbPath, auditDir); err != nil {
		t.Fatalf("second Index: %v", err)
	}

	db, err = openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var count2 int
	if err := db.QueryRow(`SELECT count(*) FROM audit_fts`).Scan(&count2); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	if count1 != count2 {
		t.Errorf("row count changed after second index: %d → %d (expected idempotency)", count1, count2)
	}
	if count1 == 0 {
		t.Error("expected rows after indexing but got 0")
	}
}

// TestQueryReturnsResults: after indexing a file with known content, Query finds it.
func TestQueryReturnsResults(t *testing.T) {
	dbPath, cleanup := makeDB(t)
	defer cleanup()

	auditDir := t.TempDir()
	lines := []map[string]interface{}{
		{
			"session_id": "sess42",
			"ts":         "2025-03-01T12:00:00Z",
			"kind":       "tool_use",
			"file_path":  "/app/server.go",
			"content":    "refactored authentication middleware for oauth2 compliance",
		},
		{
			"session_id": "sess42",
			"ts":         "2025-03-01T12:01:00Z",
			"kind":       "tool_use",
			"file_path":  "/app/db.go",
			"content":    "added postgres connection pool settings",
		},
	}
	makeAuditFile(t, auditDir, lines)

	if err := Index(dbPath, auditDir); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Query for a term present in the first entry.
	entries, err := Query(dbPath, "authentication", 20)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) < 1 {
		t.Error("expected ≥1 result for 'authentication', got 0")
	}

	// Verify preview is populated and truncated to 120 chars.
	for _, e := range entries {
		if e.Preview == "" {
			t.Error("entry has empty preview")
		}
		if len(e.Preview) > 120 {
			t.Errorf("preview too long: %d chars", len(e.Preview))
		}
		if e.SessionID == "" {
			t.Error("entry missing session_id")
		}
	}

	// Query for term that should not match.
	none, err := Query(dbPath, "xyzzy_no_match_term_999", 20)
	if err != nil {
		t.Fatalf("Query no-match: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected 0 results for unmatched term, got %d", len(none))
	}
}

// TestDBPathFallback: when CLAUDE_PLUGIN_DATA is unset, path uses ~/.pakka/recall.db.
func TestDBPathFallback(t *testing.T) {
	// Unset CLAUDE_PLUGIN_DATA to test fallback.
	orig, had := os.LookupEnv("CLAUDE_PLUGIN_DATA")
	os.Unsetenv("CLAUDE_PLUGIN_DATA")
	if had {
		defer os.Setenv("CLAUDE_PLUGIN_DATA", orig)
	}

	path, err := DBPath()
	if err != nil {
		t.Fatalf("DBPath: %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".pakka", "recall.db")
	if path != expected {
		t.Errorf("DBPath = %q, want %q", path, expected)
	}
	if !strings.HasSuffix(path, "recall.db") {
		t.Errorf("path should end with recall.db, got %q", path)
	}
}

// TestDBPathEnvVar: when CLAUDE_PLUGIN_DATA is set, use it.
func TestDBPathEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_DATA", dir)

	path, err := DBPath()
	if err != nil {
		t.Fatalf("DBPath: %v", err)
	}

	expected := filepath.Join(dir, "recall.db")
	if path != expected {
		t.Errorf("DBPath = %q, want %q", path, expected)
	}
}

// TestEmptyQueryReturnsRecent: Query with empty text returns entries ordered by ts desc.
func TestEmptyQueryReturnsRecent(t *testing.T) {
	dbPath, cleanup := makeDB(t)
	defer cleanup()

	auditDir := t.TempDir()
	lines := []map[string]interface{}{
		{"session_id": "s1", "ts": "2025-01-01T10:00:00Z", "kind": "tool_use", "file_path": "/a.go", "content": "first entry"},
		{"session_id": "s1", "ts": "2025-01-01T11:00:00Z", "kind": "tool_use", "file_path": "/b.go", "content": "second entry"},
		{"session_id": "s1", "ts": "2025-01-01T12:00:00Z", "kind": "tool_use", "file_path": "/c.go", "content": "third entry newest"},
	}
	makeAuditFile(t, auditDir, lines)

	if err := Index(dbPath, auditDir); err != nil {
		t.Fatalf("Index: %v", err)
	}

	entries, err := Query(dbPath, "", 10)
	if err != nil {
		t.Fatalf("Query empty: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries for empty query, got 0")
	}
	// First result should be the newest (ts desc).
	if len(entries) >= 2 && entries[0].Ts < entries[1].Ts {
		t.Errorf("results not in descending ts order: %s < %s", entries[0].Ts, entries[1].Ts)
	}
}
