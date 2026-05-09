// Package recall implements FTS5 indexing and querying of pakka audit logs.
//
// Uses modernc.org/sqlite (pure-Go SQLite transpile, no CGO) for cross-platform
// compatibility. go-sqlite3 requires CGO and breaks cross-compilation to linux;
// modernc.org/sqlite ships FTS5 + porter tokenizer and compiles to all targets.
//
// DB path resolution (in priority order):
//  1. $CLAUDE_PLUGIN_DATA/recall.db — survives plugin updates
//  2. ~/.pakka/recall.db            — local dev / missing env
package recall

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Entry is one row returned by Query.
type Entry struct {
	SessionID string  `json:"session_id"`
	Ts        string  `json:"ts"`
	Kind      string  `json:"kind"`
	FilePath  string  `json:"file_path"`
	Preview   string  `json:"preview"`
	Score     float64 `json:"score"`
}

// DBPath returns the resolved path to recall.db.
//
// Priority: $CLAUDE_PLUGIN_DATA/recall.db > ~/.pakka/recall.db.
// Errors: Returns error only if home dir lookup fails.
func DBPath() (string, error) {
	if dir := os.Getenv("CLAUDE_PLUGIN_DATA"); dir != "" {
		return filepath.Join(dir, "recall.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("recall: home dir: %w", err)
	}
	return filepath.Join(home, ".pakka", "recall.db"), nil
}

// openDB opens (or creates) the recall SQLite database and returns a *sql.DB.
// Creates schema if tables don't exist.
func openDB(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("recall: mkdir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("recall: open db: %w", err)
	}

	if err := applySchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// applySchema creates the FTS5 table and index-state table if absent.
func applySchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS audit_fts USING fts5(
			session_id UNINDEXED,
			ts         UNINDEXED,
			kind,
			file_path,
			content,
			tokenize = 'porter ascii'
		);
		CREATE TABLE IF NOT EXISTS audit_index_state (
			file TEXT PRIMARY KEY,
			last_offset INTEGER NOT NULL DEFAULT 0
		);
	`)
	if err != nil {
		return fmt.Errorf("recall: schema: %w", err)
	}
	return nil
}

// Index reads all *.jsonl files in auditDir and inserts new rows into the
// FTS5 table, skipping already-indexed content (idempotent by file+offset).
//
// Purpose: Called at SessionStart and SessionEnd to keep the index current.
// Errors: Returns first file-system or DB error encountered.
func Index(dbPath, auditDir string) error {
	db, err := openDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	pattern := filepath.Join(auditDir, "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("recall: glob audit dir: %w", err)
	}

	for _, f := range files {
		if err := indexFile(db, f); err != nil {
			return err
		}
	}
	return nil
}

// indexFile indexes a single JSONL file from the given offset onward.
func indexFile(db *sql.DB, path string) error {
	// Load last indexed offset for this file.
	var lastOffset int64
	row := db.QueryRow(`SELECT last_offset FROM audit_index_state WHERE file = ?`, path)
	_ = row.Scan(&lastOffset)

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("recall: open %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Seek(lastOffset, 0); err != nil {
		return fmt.Errorf("recall: seek %s: %w", path, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("recall: begin tx: %w", err)
	}
	defer tx.Rollback()

	insertStmt, err := tx.Prepare(`INSERT INTO audit_fts(session_id, ts, kind, file_path, content) VALUES (?,?,?,?,?)`)
	if err != nil {
		return fmt.Errorf("recall: prepare insert: %w", err)
	}
	defer insertStmt.Close()

	var currentOffset int64 = lastOffset

	// Read line by line without loading entire file.
	buf := make([]byte, 0, 4096)
	readBuf := make([]byte, 4096)
	for {
		n, readErr := f.Read(readBuf)
		buf = append(buf, readBuf[:n]...)

		for {
			idx := strings.IndexByte(string(buf), '\n')
			if idx < 0 {
				break
			}
			line := strings.TrimSpace(string(buf[:idx]))
			buf = buf[idx+1:]
			currentOffset += int64(idx) + 1

			if line == "" {
				continue
			}

			sid, ts, kind, filePath, content := parseAuditLine(path, line)
			if _, err := insertStmt.Exec(sid, ts, kind, filePath, content); err != nil {
				return fmt.Errorf("recall: insert row: %w", err)
			}
		}

		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				// Real error — do NOT advance offset; caller retries cleanly next run.
				return fmt.Errorf("recall: read %s: %w", path, readErr)
			}
			// EOF — flush any partial line remaining in buffer.
			if line := strings.TrimSpace(string(buf)); line != "" {
				currentOffset += int64(len(buf))
				sid, ts, kind, filePath, content := parseAuditLine(path, line)
				if _, err := insertStmt.Exec(sid, ts, kind, filePath, content); err != nil {
					return fmt.Errorf("recall: insert row: %w", err)
				}
			}
			break
		}
	}

	// Update offset tracking.
	if _, err := tx.Exec(`
		INSERT INTO audit_index_state(file, last_offset)
		VALUES(?, ?)
		ON CONFLICT(file) DO UPDATE SET last_offset = excluded.last_offset
	`, path, currentOffset); err != nil {
		return fmt.Errorf("recall: update index state: %w", err)
	}

	return tx.Commit()
}

// parseAuditLine parses one JSONL line and extracts indexable fields.
// Falls back gracefully when fields are missing.
func parseAuditLine(sourceFile, line string) (sessionID, ts, kind, filePath, content string) {
	content = line // Full JSON line is always the searchable content.

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		// Unparseable line — index raw.
		return "", "", "raw", filepath.Base(sourceFile), content
	}

	sessionID, _ = m["session_id"].(string)
	ts, _ = m["ts"].(string)

	// kind: prefer "kind", fall back to "tool_name", then "event_type".
	kind, _ = m["kind"].(string)
	if kind == "" {
		kind, _ = m["tool_name"].(string)
	}
	if kind == "" {
		kind, _ = m["event_type"].(string)
	}
	if kind == "" {
		kind = "unknown"
	}

	// file_path: look in tool_input.file_path, tool_input.command, or top-level.
	filePath, _ = m["file_path"].(string)
	if filePath == "" {
		if ti, ok := m["tool_input"].(map[string]interface{}); ok {
			filePath, _ = ti["file_path"].(string)
			if filePath == "" {
				filePath, _ = ti["command"].(string)
			}
		}
	}

	return sessionID, ts, kind, filePath, content
}

// Query searches the FTS5 index for text and returns up to limit results.
//
// Empty text returns the most recent limit entries ordered by ts desc.
// Purpose: Power the /pakka:recall command.
// Errors: Returns error on DB failure.
func Query(dbPath, text string, limit int) ([]Entry, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if limit <= 0 {
		limit = 20
	}

	var rows *sql.Rows
	if strings.TrimSpace(text) == "" {
		// No query: return most recent entries by ts.
		rows, err = db.Query(`
			SELECT session_id, ts, kind, file_path, content
			FROM audit_fts
			ORDER BY ts DESC
			LIMIT ?
		`, limit)
	} else {
		// FTS match ordered by BM25 rank.
		rows, err = db.Query(`
			SELECT session_id, ts, kind, file_path, content, -rank AS score
			FROM audit_fts
			WHERE audit_fts MATCH ?
			ORDER BY rank
			LIMIT ?
		`, text, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("recall: query: %w", err)
	}
	defer rows.Close()

	var results []Entry
	isScored := strings.TrimSpace(text) != ""

	for rows.Next() {
		var e Entry
		var content string
		var score sql.NullFloat64
		if isScored {
			if err := rows.Scan(&e.SessionID, &e.Ts, &e.Kind, &e.FilePath, &content, &score); err != nil {
				return nil, fmt.Errorf("recall: scan: %w", err)
			}
			e.Score = score.Float64
		} else {
			if err := rows.Scan(&e.SessionID, &e.Ts, &e.Kind, &e.FilePath, &content); err != nil {
				return nil, fmt.Errorf("recall: scan: %w", err)
			}
		}
		// Preview: first 120 chars of content.
		if len(content) > 120 {
			e.Preview = content[:120]
		} else {
			e.Preview = content
		}
		results = append(results, e)
	}
	return results, rows.Err()
}
