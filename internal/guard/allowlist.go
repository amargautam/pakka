// Per-repo learned allowlist for guard false-positive reduction (issue #12).
//
// When guard blocks a Bash heuristic match (eval, traversal, …) the user may
// override via the PreToolUse "ask" decision. The override is recorded in
// <repo>/.pakka/guard-allowlist.json keyed by pattern + normalized command
// shape; identical future commands pass with an audit note. High override
// counts demote a pattern from block to warn; old overrides decay out.
//
// Security invariants:
//   - Path-based secret denials (.env*, ~/.ssh, ~/.aws, …) and the
//     system-path Bash deny never consult the allowlist.
//   - Malformed allowlist JSON fails closed: guard blocks, file untouched.
//   - The allowlist file itself is guard write-protected.
package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SchemaV1 is the recognized allowlist schema identifier.
const SchemaV1 = "pakka.guard-allowlist.v1"

// allowlistFile is the per-repo allowlist filename under .pakka/.
const allowlistFile = "guard-allowlist.json"

// pendingTTL is how long an unconsumed override marker stays valid.
const pendingTTL = 30 * time.Minute

// Config holds guard allowlist thresholds. Zero values are replaced by
// DefaultConfig() at the consult sites.
type Config struct {
	// DemoteThreshold: overrides within the decay window at or above which a
	// pattern is demoted block → warn for the repo.
	DemoteThreshold int
	// DecayWindowDays: overrides older than this stop counting toward
	// demotion and their shapes expire from the allowlist.
	DecayWindowDays int
}

// DefaultConfig returns the sane-default thresholds.
func DefaultConfig() Config {
	return Config{DemoteThreshold: 5, DecayWindowDays: 30}
}

func (c Config) normalized() Config {
	d := DefaultConfig()
	if c.DemoteThreshold <= 0 {
		c.DemoteThreshold = d.DemoteThreshold
	}
	if c.DecayWindowDays <= 0 {
		c.DecayWindowDays = d.DecayWindowDays
	}
	return c
}

// Allowlist is the on-disk shape of .pakka/guard-allowlist.json.
type Allowlist struct {
	Schema    string                   `json:"schema"`
	LastDecay string                   `json:"last_decay,omitempty"`
	Patterns  map[string]*PatternEntry `json:"patterns"`
}

// PatternEntry tracks one guard pattern's mode and recorded overrides.
type PatternEntry struct {
	Mode   string                 `json:"mode"` // "block" (default) or "warn"
	Shapes map[string]*ShapeEntry `json:"shapes,omitempty"`
}

// ShapeEntry counts user overrides for one normalized command shape.
type ShapeEntry struct {
	Count   int    `json:"count"`
	FirstTS string `json:"first_ts"`
	LastTS  string `json:"last_ts"`
}

// allowlistablePatterns are the Bash heuristic categories a user may
// override. Path-based secret denials and system-path access are NEVER here.
var allowlistablePatterns = map[string]bool{
	"eval":          true,
	"shell-c-eval":  true,
	"pipe-shell":    true,
	"download-exec": true,
	"traversal":     true,
}

var wsRe = regexp.MustCompile(`\s+`)

// Shape normalizes a command for allowlist matching: whitespace runs
// collapse to single spaces, surrounding whitespace trimmed. Deliberately
// conservative — only the identical command (modulo whitespace) matches.
func Shape(cmd string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(cmd, " "))
}

// InputHash returns the 16-hex-char prefix of sha256(toolInput), used to key
// pending override markers between the PreToolUse and PostToolUse hooks.
func InputHash(toolInput []byte) string {
	h := sha256.Sum256(toolInput)
	return hex.EncodeToString(h[:])[:16]
}

// RepoRoot resolves the per-repo scope for cwd: the nearest ancestor
// containing .git. $HOME is never a valid root (it holds pakka's own data
// dir, and a dotfiles repo there would silently globalize the allowlist).
// Falls back to cwd itself when no marker is found; empty cwd → "".
func RepoRoot(cwd string) string {
	if cwd == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		home = filepath.Clean(home)
	}
	dir := filepath.Clean(cwd)
	for i := 0; i < 64; i++ {
		if dir != home {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Clean(cwd)
}

// Load reads the repo's allowlist. Absent file → (nil, nil). Malformed JSON
// or unknown schema → (nil, error): callers must fail closed (keep blocking)
// and must never overwrite the file.
func Load(root string) (*Allowlist, error) {
	if root == "" {
		return nil, nil
	}
	b, err := os.ReadFile(filepath.Join(root, ".pakka", allowlistFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var al Allowlist
	if err := json.Unmarshal(b, &al); err != nil {
		return nil, fmt.Errorf("malformed guard allowlist: %w", err)
	}
	if al.Schema != "" && al.Schema != SchemaV1 {
		return nil, fmt.Errorf("unknown guard allowlist schema %q", al.Schema)
	}
	if al.Patterns == nil {
		al.Patterns = map[string]*PatternEntry{}
	}
	// Drop entries for categories that are never allowlistable — they cannot
	// weaken the secrets/system deny rules even if hand-written.
	for name := range al.Patterns {
		if !allowlistablePatterns[name] {
			delete(al.Patterns, name)
		}
	}
	return &al, nil
}

// Save writes the allowlist atomically (temp file + rename) under
// <root>/.pakka/.
func Save(root string, al *Allowlist) error {
	dir := filepath.Join(root, ".pakka")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	al.Schema = SchemaV1
	b, err := json.MarshalIndent(al, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".guard-allowlist-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, 0644)
	return os.Rename(tmpName, filepath.Join(dir, allowlistFile))
}

// RecordOverride increments the override count for pattern+shape in the
// repo's allowlist, creating the file if needed, then runs the decay pass
// (demotion at threshold, expiry, re-promotion) and persists. This is the
// only place the allowlist is written — guard evaluation never persists.
//
// Errors: non-allowlistable pattern; malformed existing file (fail closed —
// the file is left untouched); filesystem failures.
func RecordOverride(root, pattern, shape string, cfg Config, now time.Time) error {
	if !allowlistablePatterns[pattern] {
		return fmt.Errorf("pattern %q is not allowlistable", pattern)
	}
	if root == "" {
		return fmt.Errorf("empty repo root")
	}
	cfg = cfg.normalized()
	al, err := Load(root)
	if err != nil {
		return err
	}
	ts := now.UTC().Format(time.RFC3339)
	if al == nil {
		al = &Allowlist{Schema: SchemaV1, Patterns: map[string]*PatternEntry{}}
	}
	pe := al.Patterns[pattern]
	if pe == nil {
		pe = &PatternEntry{Mode: "block", Shapes: map[string]*ShapeEntry{}}
		al.Patterns[pattern] = pe
	}
	if pe.Shapes == nil {
		pe.Shapes = map[string]*ShapeEntry{}
	}
	se := pe.Shapes[shape]
	if se == nil {
		pe.Shapes[shape] = &ShapeEntry{Count: 1, FirstTS: ts, LastTS: ts}
	} else {
		se.Count++
		se.LastTS = ts
	}
	MaintainDecay(al, cfg, now)
	al.LastDecay = ts
	return Save(root, al)
}

// windowCount sums override counts whose last override falls within the
// decay window.
func windowCount(pe *PatternEntry, cfg Config, now time.Time) int {
	window := time.Duration(cfg.DecayWindowDays) * 24 * time.Hour
	total := 0
	for _, se := range pe.Shapes {
		t, err := time.Parse(time.RFC3339, se.LastTS)
		if err != nil || now.Sub(t) > window {
			continue
		}
		total += se.Count
	}
	return total
}

// MaintainDecay runs the override-count decay pass in memory: shapes whose
// last override is outside the decay window expire; each pattern's mode is
// recomputed (warn at/above threshold, block below — including
// re-promotion). It never touches disk: consult applies it to the loaded
// state on every evaluation (cheap — entries are few), and RecordOverride
// persists the decayed state. Returns true when anything changed.
func MaintainDecay(al *Allowlist, cfg Config, now time.Time) bool {
	if al == nil {
		return false
	}
	cfg = cfg.normalized()
	window := time.Duration(cfg.DecayWindowDays) * 24 * time.Hour
	changed := false
	for name, pe := range al.Patterns {
		for shape, se := range pe.Shapes {
			t, err := time.Parse(time.RFC3339, se.LastTS)
			if err != nil || now.Sub(t) > window {
				delete(pe.Shapes, shape)
				changed = true
			}
		}
		mode := "block"
		if windowCount(pe, cfg, now) >= cfg.DemoteThreshold {
			mode = "warn"
		}
		if pe.Mode != mode {
			pe.Mode = mode
			changed = true
		}
		if len(pe.Shapes) == 0 && pe.Mode == "block" {
			delete(al.Patterns, name)
			changed = true
		}
	}
	return changed
}

// --- consult (called from checkBash) ---

type allowlistVerdict int

const (
	verdictNone allowlistVerdict = iota // no entry — block stands
	verdictShape                        // exact shape allowlisted
	verdictWarn                         // pattern demoted to warn
)

// consultAllowlist checks the repo allowlist for pattern+shape after
// applying the decay pass in memory (no disk write on the guard hot path —
// persistence happens only in RecordOverride). Malformed file → verdictNone
// plus a non-empty error string (fail closed).
func consultAllowlist(root, pattern, shape string, cfg Config, now time.Time) (allowlistVerdict, string) {
	al, err := Load(root)
	if err != nil {
		return verdictNone, err.Error()
	}
	if al == nil {
		return verdictNone, ""
	}
	MaintainDecay(al, cfg, now)
	pe := al.Patterns[pattern]
	if pe == nil {
		return verdictNone, ""
	}
	if pe.Mode == "warn" {
		return verdictWarn, ""
	}
	if pe.Shapes[shape] != nil {
		return verdictShape, ""
	}
	return verdictNone, ""
}

// --- pending override markers (PreToolUse ask → PostToolUse record) ---

// PendingOverride is the marker guard writes when it asks the user to
// approve a blocked command. The PostToolUse audit hook consumes it: if the
// tool ran, the user approved — record the override.
type PendingOverride struct {
	Pattern   string `json:"pattern"`
	Shape     string `json:"shape"`
	Root      string `json:"root"`
	SessionID string `json:"session_id"`
	TS        string `json:"ts"`
}

func pendingDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pakka", "guard", "pending"), nil
}

// WritePendingOverride persists a marker keyed by the tool-input hash and
// opportunistically sweeps stale markers (denied asks are never consumed, so
// without the sweep the pending dir would grow unbounded).
func WritePendingOverride(inputHash string, po PendingOverride, now time.Time) error {
	dir, err := pendingDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	sweepPending(dir, now)
	po.TS = now.UTC().Format(time.RFC3339)
	b, err := json.Marshal(po)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, inputHash+".json"), b, 0600)
}

// sweepPending removes marker files older than pendingTTL (by mtime).
// Best-effort; the dir holds at most a handful of entries.
func sweepPending(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > pendingTTL {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// RecordApprovedOverride is the PostToolUse entry point: it consumes the
// pending marker for inputHash and records the override only if the marker
// is consistent with what actually ran.
//
// The pending dir is not write-protected, so markers are treated as hints,
// never as authority. Validation:
//   - session matches and marker is fresh (ConsumeOverride)
//   - marker pattern is allowlistable AND its regex matches the executed
//     command — a marker forged for an innocuous command can never validate,
//     and a command that does match a pattern only runs after the user
//     approves guard's "ask" prompt
//   - marker shape equals Shape(command)
//   - marker root equals RepoRoot(cwd) of the event that ran — a marker
//     cannot plant an allowlist into a directory the command didn't run in
//
// Returns (pattern, recorded, error). (pattern, false, error) = marker
// existed but failed validation or persistence; ("", false, nil) = nothing
// pending.
func RecordApprovedOverride(inputHash, sessionID, command, cwd string, cfg Config, now time.Time) (string, bool, error) {
	po, ok := ConsumeOverride(inputHash, sessionID, now)
	if !ok {
		return "", false, nil
	}
	chk := bashCheckFor(po.Pattern)
	if chk == nil || !chk.allowlistable {
		return po.Pattern, false, fmt.Errorf("override marker rejected: pattern %q is not allowlistable", po.Pattern)
	}
	if !chk.re.MatchString(command) || Shape(command) != po.Shape {
		return po.Pattern, false, fmt.Errorf("override marker rejected: marker does not match executed command")
	}
	if root := RepoRoot(cwd); root == "" || root != po.Root {
		return po.Pattern, false, fmt.Errorf("override marker rejected: root mismatch")
	}
	if err := RecordOverride(po.Root, po.Pattern, po.Shape, cfg, now); err != nil {
		return po.Pattern, false, err
	}
	return po.Pattern, true, nil
}

// ConsumeOverride reads and removes the marker for inputHash. Returns
// (marker, true) only when the session matches and the marker is younger
// than pendingTTL. Stale markers are deleted; foreign-session markers are
// left for their own session.
func ConsumeOverride(inputHash, sessionID string, now time.Time) (*PendingOverride, bool) {
	dir, err := pendingDir()
	if err != nil {
		return nil, false
	}
	path := filepath.Join(dir, inputHash+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var po PendingOverride
	if err := json.Unmarshal(b, &po); err != nil {
		_ = os.Remove(path)
		return nil, false
	}
	t, err := time.Parse(time.RFC3339, po.TS)
	if err != nil || now.Sub(t) > pendingTTL {
		_ = os.Remove(path)
		return nil, false
	}
	if po.SessionID != sessionID {
		return nil, false
	}
	_ = os.Remove(path)
	return &po, true
}
