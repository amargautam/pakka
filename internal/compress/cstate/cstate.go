// Package cstate is the single, dependency-free reader of the compress-state
// file's stale count. It exists so both the orchestrator (which writes the file
// and owns the full State type) and the hot-path binary (cmd/pakka-hot, via
// internal/hotcli) share ONE decoder of the on-disk stale semantics — no
// hand-copied second parser that could drift.
//
// It imports only the standard library (encoding/json, os, path/filepath), so
// linking it never pulls in internal/compress/semantic → net/http. That is what
// lets internal/hotcli read the stale glyph without the ~1.8ms net/http
// startup-floor cost. See docs/specs/2026-07-24-hot-path-startup-floor.md.
package cstate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FileName is the on-disk filename used inside <repo>/.pakka/.
const FileName = "compress-state.json"

// Path returns the canonical compress-state file path for a repo directory.
func Path(repoDir string) string {
	return filepath.Join(repoDir, ".pakka", FileName)
}

// staleEntry decodes only the field the stale count reads. It is the single
// source of truth for "what does stale mean on disk": an entry whose validator
// did not pass. The orchestrator's writer stamps the same `validatorPasses`
// key; keep the two in sync if the schema's stale semantics ever change.
type staleEntry struct {
	ValidatorPasses bool `json:"validatorPasses"`
}

// CountStaleFromDisk reads <repoDir>/.pakka/compress-state.json and returns the
// number of entries whose validator did not pass — the `! N stale` status-line
// glyph. Returns 0 on any read/parse failure (an empty/missing/corrupt file
// must never block status-line or commit-gate).
func CountStaleFromDisk(repoDir string) int {
	if repoDir == "" {
		return 0
	}
	data, err := os.ReadFile(Path(repoDir))
	if err != nil {
		return 0
	}
	var raw map[string]staleEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0
	}
	n := 0
	for _, e := range raw {
		if !e.ValidatorPasses {
			n++
		}
	}
	return n
}
