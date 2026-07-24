package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amargautam/pakka/internal/cli"
	"github.com/amargautam/pakka/internal/recall"
)

// recall_cmd.go is the ONLY file that imports internal/recall (and therefore the
// only sqlite link in any pakka binary). It lives in cmd/pakka-core — not in
// internal/cli — so the lean cmd/pakka-hot binary can import cli without pulling
// modernc.org/sqlite in transitively. main() wires these handlers into the
// shared dispatcher via cli.IndexFunc / cli.QueryFunc.

// --- index (Pass 6) ---

// runRecallIndex indexes all audit JSONL files into the recall DB.
//
// Purpose: Called at SessionStart and SessionEnd to keep FTS5 index current.
// Skips gracefully if pakka.recall.enabled = false.
// Exits 0 on success (or skip); exits 1 on error.
func runRecallIndex() error {
	if !cli.RecallEnabled() {
		return nil
	}

	dbPath, err := recall.DBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: index: %v\n", err)
		os.Exit(1)
	}

	auditDir, err := defaultAuditDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: index: %v\n", err)
		os.Exit(1)
	}

	if err := recall.Index(dbPath, auditDir); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: index: %v\n", err)
		os.Exit(1)
	}
	return nil
}

// --- query (Pass 6) ---

// runRecallQuery searches the FTS5 index and prints matching rows as JSON lines.
//
// Purpose: Power /pakka:recall. Empty query returns last 10 entries by ts desc.
// Skips gracefully if pakka.recall.enabled = false.
// Exits 0 always (no results is not an error).
func runRecallQuery(args []string) error {
	if !cli.RecallEnabled() {
		return nil
	}

	dbPath, err := recall.DBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: query: %v\n", err)
		os.Exit(1)
	}

	text := strings.Join(args, " ")
	limit := 20
	if text == "" {
		limit = 10
	}

	entries, err := recall.Query(dbPath, text, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: query: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			fmt.Fprintf(os.Stderr, "pakka: query: encode: %v\n", err)
			os.Exit(1)
		}
	}
	return nil
}

// --- helpers ---

// defaultAuditDir returns ~/.pakka/audit.
func defaultAuditDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".pakka", "audit"), nil
}
