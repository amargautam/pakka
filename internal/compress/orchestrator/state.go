// Package orchestrator drives session-start auto-compression of allowlisted
// memory files via the semantic LLM rewriter.
//
// The orchestrator is invoked in two ways:
//
//  1. Synchronously, by `/pakka:compress <level>` to re-compress every
//     allowlisted target at the freshly-set level.
//  2. Asynchronously, at SessionStart — the parent hook returns within ~50ms
//     after forking a detached `pakka-core compress --orchestrator-bg` process
//     that runs the same Run path and writes its log to
//     ~/.pakka/orchestrator.log.
//
// State per repo lives in <repo>/.pakka/compress-state.json. The file records
// the source SHA256, the level used, the compress timestamp, and whether the
// validator passed. Stale entries (level changed, source changed, or prior
// validator failure) are re-compressed on the next eligible run; status-line
// surfaces the count of failed entries via a `! N stale` segment.
package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// StateFileName is the on-disk filename used inside <repo>/.pakka/.
const StateFileName = "compress-state.json"

// Entry is one record in the state map.
type Entry struct {
	SourceSHA        string `json:"sourceSHA"`
	Level            string `json:"level"`
	CompressedAt     string `json:"compressedAt"`
	ValidatorPasses  bool   `json:"validatorPasses"`
}

// State is the in-memory map keyed by absolute target file path.
//
// State is safe for concurrent reads after Load; mutations should be guarded
// by the caller (the orchestrator owns its own sync.Mutex).
type State struct {
	mu      sync.Mutex
	entries map[string]Entry
}

// NewState returns an empty State.
//
// Purpose: Construct a fresh state for tests and first-time loads.
// Errors: None.
func NewState() *State {
	return &State{entries: make(map[string]Entry)}
}

// LoadState reads <repo>/.pakka/compress-state.json. Missing file → empty
// state with nil error (first run is the common case).
//
// Purpose: Hydrate orchestrator state on each invocation.
// Errors: Returns a wrapped JSON error when the file exists but is corrupt.
func LoadState(repoDir string) (*State, error) {
	path := statePath(repoDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewState(), nil
		}
		return nil, fmt.Errorf("orchestrator: read state: %w", err)
	}
	if len(data) == 0 {
		return NewState(), nil
	}
	var raw map[string]Entry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("orchestrator: parse state: %w", err)
	}
	if raw == nil {
		raw = make(map[string]Entry)
	}
	return &State{entries: raw}, nil
}

// Save writes <repo>/.pakka/compress-state.json atomically (tmp + rename).
// Keys are sorted so the on-disk file is byte-stable across rewrites.
//
// Purpose: Persist orchestrator decisions across runs.
// Errors: Returns wrapped FS errors. Does not fsync the rename target dir;
// state is advisory and a torn write is recoverable from the next run.
func (s *State) Save(repoDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(repoDir, ".pakka")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("orchestrator: mkdir state: %w", err)
	}
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]Entry, len(keys))
	for _, k := range keys {
		ordered[k] = s.entries[k]
	}
	data, err := jsonMarshalSorted(ordered, keys)
	if err != nil {
		return fmt.Errorf("orchestrator: marshal state: %w", err)
	}
	finalPath := statePath(repoDir)
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("orchestrator: write tmp state: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("orchestrator: rename state: %w", err)
	}
	return nil
}

// Stale returns true when the orchestrator should (re)compress the file.
//
// Purpose: Decide whether a target needs recompression on this pass.
// A file is stale when:
//   - There is no recorded entry, OR
//   - The recorded level differs from currentLevel, OR
//   - The recorded sourceSHA differs from sourceSHA, OR
//   - The previous validator pass was false (retry).
// Errors: None.
func (s *State) Stale(absPath, currentLevel, sourceSHA string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[absPath]
	if !ok {
		return true
	}
	if e.Level != currentLevel {
		return true
	}
	if e.SourceSHA != sourceSHA {
		return true
	}
	if !e.ValidatorPasses {
		return true
	}
	return false
}

// Record updates the entry for absPath. Caller drives the timestamp through
// the supplied compressedAt string (RFC3339 UTC). The Save method must be
// called separately to persist.
//
// Purpose: Mark a successful (or failed) compression in memory.
// Errors: None.
func (s *State) Record(absPath, level, sourceSHA, compressedAt string, validatorPasses bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[absPath] = Entry{
		SourceSHA:       sourceSHA,
		Level:           level,
		CompressedAt:    compressedAt,
		ValidatorPasses: validatorPasses,
	}
}

// Get returns the entry for absPath and a found flag.
//
// Purpose: Test helper and external callers (orchestrator-status command).
// Errors: None.
func (s *State) Get(absPath string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[absPath]
	return e, ok
}

// All returns a copy of the entries map sorted by key.
//
// Purpose: Render `pakka-core orchestrator-status` and the stale-glyph count.
// Errors: None.
func (s *State) All() []KeyedEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]KeyedEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, KeyedEntry{Path: k, Entry: s.entries[k]})
	}
	return out
}

// CountStale returns the number of entries where ValidatorPasses == false.
//
// Purpose: Used by status-line to render `! N stale` glyph.
// Errors: None.
func (s *State) CountStale() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.entries {
		if !e.ValidatorPasses {
			n++
		}
	}
	return n
}

// KeyedEntry pairs a path with its Entry for All().
type KeyedEntry struct {
	Path  string
	Entry Entry
}

// statePath returns the canonical state file path for a repo directory.
func statePath(repoDir string) string {
	return filepath.Join(repoDir, ".pakka", StateFileName)
}

// jsonMarshalSorted writes a map using a fixed key order so the byte output
// is stable across runs. encoding/json marshals maps in lexicographic order
// already, but we route through this helper to keep the contract explicit
// (and to make it trivial to swap in a custom encoder later).
func jsonMarshalSorted(m map[string]Entry, keys []string) ([]byte, error) {
	// encoding/json sorts map keys lexicographically — keys is already sorted,
	// so the standard encoder produces the same byte output. We use
	// MarshalIndent for human-readable diffs in the repo state file.
	_ = keys
	return json.MarshalIndent(m, "", "  ")
}

// CountStaleFromDisk reads the state file at repoDir and returns the stale
// count without holding any orchestrator state in memory.
//
// Purpose: status-line invokes this without paying for a full Load+lock dance.
// Errors: Returns 0 on any read or parse failure (status-line must never block).
func CountStaleFromDisk(repoDir string) int {
	if repoDir == "" {
		return 0
	}
	data, err := os.ReadFile(statePath(repoDir))
	if err != nil {
		return 0
	}
	var raw map[string]Entry
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
