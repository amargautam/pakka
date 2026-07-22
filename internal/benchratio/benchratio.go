// Package benchratio persists and resolves measured output-reduction ratios
// produced by the `make bench` A/B harness.
//
// The status line and RECEIPTS self-report historically derived output savings
// from a fixed per-level multiplier calibrated once (2026-05-02). This package
// replaces that folklore with measurement: each A/B bench run records the
// observed reduction fraction (1 - pakka_output/raw_output) keyed by
// repo+model+level. Readers resolve the multiplier most-specific-first —
// repo+model+level, then model+level — falling back to the calibrated constant
// only when no measurement exists.
//
// Storage: ~/.pakka/bench-ratios.json. Home resolution follows the meter /
// statusline OverrideHome precedent so tests can redirect it.
package benchratio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// OverrideHome, when non-empty, substitutes for os.UserHomeDir(). Used by
// tests to redirect the ~/.pakka/bench-ratios.json lookup.
var OverrideHome string

// Entry is one measured reduction ratio for a repo+model+level key.
//
// Ratio is the output-reduction FRACTION in [0,1): 1 - pakka_out/raw_out.
// Samples counts how many bench runs contributed (Record increments it and
// keeps Ratio as the running mean). UpdatedAt is an RFC3339 timestamp supplied
// by the caller (the package never reads the clock, for deterministic tests).
type Entry struct {
	Repo      string  `json:"repo"`
	Model     string  `json:"model"`
	Level     string  `json:"level"`
	Ratio     float64 `json:"ratio"`
	Samples   int     `json:"samples"`
	UpdatedAt string  `json:"updated_at"`
}

// Store is the on-disk document: a flat list of entries.
type Store struct {
	Entries []Entry `json:"entries"`
}

// Clamp constrains a raw reduction fraction to [0,1). Negative values (pakka
// arm emitted MORE than raw) clamp to 0; values at or above 1 (raw emitted
// nothing, degenerate) clamp just below 1 so a stored ratio never reads as a
// literal 100% reduction.
func Clamp(r float64) float64 {
	if r < 0 {
		return 0
	}
	const max = 0.9999
	if r > max {
		return max
	}
	return r
}

// resolveHome returns OverrideHome if set, else os.UserHomeDir().
func resolveHome() string {
	if OverrideHome != "" {
		return OverrideHome
	}
	h, _ := os.UserHomeDir()
	return h
}

// DefaultPath returns the bench-ratios.json path under the given home.
func DefaultPath(home string) string {
	return filepath.Join(home, ".pakka", "bench-ratios.json")
}

// LoadFrom reads the store rooted at home. A missing file yields an empty
// store and no error (first-run is not a failure). Malformed JSON also yields
// an empty store so a corrupt file degrades to the calibrated constant rather
// than breaking the status line.
func LoadFrom(home string) (*Store, error) {
	s := &Store{}
	data, err := os.ReadFile(DefaultPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	_ = json.Unmarshal(data, s)
	return s, nil
}

// Load reads the store from the resolved home (OverrideHome or $HOME).
func Load() (*Store, error) { return LoadFrom(resolveHome()) }

// --- render-path cache (finding 2) -------------------------------------------
//
// resolveOutputReduction runs on the status-line hot path (50ms p95 budget).
// LoadCached memoizes the parsed store keyed by the file's mtime+size, so an
// unchanged bench-ratios.json is unmarshalled once per process instead of on
// every call. Mirrors the meterCache mtime+size invalidation in statusline.
var (
	cacheMu    sync.Mutex
	cacheKey   string // "path|mtimeNano|size" of the last parsed file
	cacheVal   *Store
	parseCount int // test hook: real Unmarshals performed via LoadCached
)

// ResetCache clears the LoadCached memo. Intended for tests (and any caller)
// that rewrite bench-ratios.json within a single process, where mtime+size may
// not change between writes and would otherwise serve a stale parse.
func ResetCache() {
	cacheMu.Lock()
	cacheKey = ""
	cacheVal = nil
	cacheMu.Unlock()
}

// LoadCached returns the store for home, reusing a memoized parse when the
// file's mtime+size are unchanged since the last LoadCached call. A missing
// file yields an empty store (not cached). The returned *Store is shared and
// MUST NOT be mutated by callers — the status line only reads it (Resolve).
func LoadCached(home string) (*Store, error) {
	path := DefaultPath(home)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{}, nil
		}
		return &Store{}, err
	}
	key := fmt.Sprintf("%s|%d|%d", path, info.ModTime().UnixNano(), info.Size())

	cacheMu.Lock()
	defer cacheMu.Unlock()
	if key == cacheKey && cacheVal != nil {
		return cacheVal, nil
	}
	s, err := LoadFrom(home)
	if err != nil {
		return s, err
	}
	parseCount++
	cacheKey = key
	cacheVal = s
	return s, nil
}

// SaveTo writes the store under home, creating ~/.pakka if needed. The write
// is atomic (temp file + rename) so a concurrent reader never sees a partial
// document.
func (s *Store) SaveTo(home string) error {
	path := DefaultPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Save writes the store to the resolved home.
func (s *Store) Save() error { return s.SaveTo(resolveHome()) }

// --- advisory lock for concurrent bench writers (finding 1) ------------------

const (
	// lockStaleAfter reclaims a lock whose file is older than this — a crashed
	// writer must not wedge future runs forever.
	lockStaleAfter = 30 * time.Second
	// lockWaitTimeout bounds how long Update blocks for a contended lock.
	lockWaitTimeout = 5 * time.Second
	// lockRetryEvery is the poll interval while a lock is held by another run.
	lockRetryEvery = 5 * time.Millisecond
)

// lockSleep is time.Sleep, indirected so tests can make retries instant.
var lockSleep = time.Sleep

// fileLock is an O_EXCL advisory lock beside bench-ratios.json (same stdlib
// contract as orchestrator.acquireFileLock; no external deps).
type fileLock struct{ path string }

func lockPath(home string) string { return DefaultPath(home) + ".lock" }

// acquireLock takes the advisory lock for home, retrying until lockWaitTimeout.
// A lock file older than lockStaleAfter is treated as abandoned and reclaimed.
func acquireLock(home string) (*fileLock, error) {
	path := lockPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "pid=%d\n", os.Getpid())
			_ = f.Close()
			return &fileLock{path: path}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// Held: reclaim if stale, else wait.
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("bench-ratios lock busy after %s", lockWaitTimeout)
		}
		lockSleep(lockRetryEvery)
	}
}

func (l *fileLock) release() {
	if l != nil && l.path != "" {
		_ = os.Remove(l.path)
	}
}

// UpdateFrom performs a locked load-modify-save on the store under home: it
// takes the advisory lock, loads the current store, applies mutate, and saves
// — so concurrent bench runs serialize instead of clobbering each other's
// sample. mutate must not retain the *Store after it returns.
func UpdateFrom(home string, mutate func(*Store)) error {
	lock, err := acquireLock(home)
	if err != nil {
		return err
	}
	defer lock.release()

	s, err := LoadFrom(home)
	if err != nil {
		return err
	}
	mutate(s)
	return s.SaveTo(home)
}

// Update runs UpdateFrom against the resolved home (OverrideHome or $HOME).
func Update(mutate func(*Store)) error { return UpdateFrom(resolveHome(), mutate) }

// Record merges a new ratio observation for repo+model+level. An existing key
// keeps its running mean: with n prior samples and mean m, the updated mean is
// (m*n + ratio)/(n+1) and Samples becomes n+1. A new key is appended with
// Samples=1. now is the RFC3339 timestamp to stamp on the touched entry.
func (s *Store) Record(repo, model, level string, ratio float64, now string) {
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.Repo == repo && e.Model == model && e.Level == level {
			n := float64(e.Samples)
			e.Ratio = (e.Ratio*n + ratio) / (n + 1)
			e.Samples++
			e.UpdatedAt = now
			return
		}
	}
	s.Entries = append(s.Entries, Entry{
		Repo: repo, Model: model, Level: level,
		Ratio: ratio, Samples: 1, UpdatedAt: now,
	})
}

// Resolve returns the measured reduction fraction for repo+model+level using
// the spec's resolution order:
//
//  1. repo+model+level  — entries matching this repo and level (and model,
//     when a non-empty model is supplied).
//  2. model+level       — entries matching this level across any repo (and
//     model, when supplied).
//
// A supplied model of "" is treated as a wildcard: the status line and
// RECEIPTS do not reliably know the session model, so they still benefit from
// measurements recorded under the concrete model the bench observed. When
// several entries match a tier, the result is their sample-weighted mean and
// samples is the total sample count. found=false means no measurement exists
// and the caller must use the calibrated constant.
func (s *Store) Resolve(repo, model, level string) (ratio float64, samples int, found bool) {
	// Tier 1: repo + level (+ model when provided).
	if r, n, ok := s.aggregate(func(e Entry) bool {
		return e.Repo == repo && e.Level == level && (model == "" || e.Model == model)
	}); ok {
		return r, n, true
	}
	// Tier 2: level across any repo (+ model when provided).
	if r, n, ok := s.aggregate(func(e Entry) bool {
		return e.Level == level && (model == "" || e.Model == model)
	}); ok {
		return r, n, true
	}
	return 0, 0, false
}

// aggregate returns the sample-weighted mean ratio and total samples over
// entries matching pred. ok=false when no entry matches (or matches carry no
// samples).
func (s *Store) aggregate(pred func(Entry) bool) (ratio float64, samples int, ok bool) {
	var weighted float64
	var total int
	for _, e := range s.Entries {
		if !pred(e) || e.Samples <= 0 {
			continue
		}
		weighted += e.Ratio * float64(e.Samples)
		total += e.Samples
	}
	if total == 0 {
		return 0, 0, false
	}
	return weighted / float64(total), total, true
}
