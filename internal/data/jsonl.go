// Package data provides shared helpers for JSONL file I/O used across
// audit, meter, and report packages.
package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// AppendJSONL marshals entry as JSON and appends it (with newline) to path.
// Creates parent directories and the file if they don't exist.
//
// Purpose: Single open-append-marshal-write helper shared by audit, meter.
// Errors: Returns error on mkdir, open, marshal, or write failure.
func AppendJSONL(path string, entry any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadLines reads a JSONL file and returns non-empty trimmed lines.
// Returns error if the file cannot be read.
//
// Purpose: Shared line-reader for report.go gather functions.
// Errors: Returns error if the file cannot be read.
func ReadLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}
