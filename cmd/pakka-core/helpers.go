package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/amargautam/pakka/internal/hookevent"
)

// parseStrict reads and parses a hook event from r, writing parse errors to w.
//
// Contract for hard-fail callers (guard, commit-gate, stack-gate):
//   - empty input → (nil, true):  silent skip, caller should return/no-op
//   - valid JSON  → (*Event, true): proceed normally
//   - malformed   → (nil, false): caller must exit non-zero; error written to w
func parseStrict(r io.Reader, w io.Writer) (*hookevent.Event, bool) {
	data, _ := io.ReadAll(r)
	if len(data) == 0 {
		return nil, true // silent skip
	}
	var event hookevent.Event
	if err := json.Unmarshal(data, &event); err != nil {
		fmt.Fprintf(w, "pakka: malformed hook event: %v\n", err)
		return nil, false
	}
	return &event, true
}

// parseLenient reads and parses a hook event from r.
//
// Contract for silent callers (meter, audit, statusline, compress):
//   - always returns a non-nil *Event
//   - on empty or malformed input, SessionID is synthesized as "sess-<unix>"
func parseLenient(r io.Reader) *hookevent.Event {
	data, _ := io.ReadAll(r)
	var event hookevent.Event
	_ = json.Unmarshal(data, &event)
	if event.SessionID == "" {
		event.SessionID = fmt.Sprintf("sess-%d", time.Now().Unix())
	}
	return &event
}

// debugLogf appends a timestamped line to ~/.pakka/debug.log.
// Failures are silently ignored — debug logging must never break the hook.
func debugLogf(format string, args ...interface{}) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".pakka")
	_ = os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, "debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), msg)
}

// pluginRoot returns the directory two levels above the running executable.
// Binary is at <root>/bin/pakka-core-<os>-<arch>; root is two levels up.
func pluginRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(filepath.Dir(exe))
}

// loadSettings reads and parses settings.json from the plugin root.
//
// Purpose: Shared config loader for output compression subcommands.
// Errors: Returns zero-value settingsJSON on any read/parse failure.
func loadSettings() settingsJSON {
	root := pluginRoot()
	data, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if err != nil {
		return settingsJSON{}
	}
	var s settingsJSON
	_ = json.Unmarshal(data, &s)
	return s
}

// loadOutputLevel returns the configured output compression level.
// Falls back to "super-ultra" if not set or invalid.
//
// Purpose: Determine output compression intensity for rules and reinforcement.
// Errors: Never errors; invalid values map to "super-ultra" (intentional default).
func loadOutputLevel() string {
	s := loadSettings()
	return resolveOutputLevel(s.Pakka.Compress.OutputLevel)
}

// resolveOutputLevel applies the defaulting policy for an output-compression
// level string. Legal values (`lite|strict|ultra|super-ultra`) round-trip
// unchanged; everything else — empty, garbage, legacy values like "audit" or
// "fast" — collapses to the brand default `super-ultra`.
//
// Purpose: Single source of truth for the "what's the active level" question.
// Errors: Never errors; invalid values map to "super-ultra".
func resolveOutputLevel(raw string) string {
	switch raw {
	case "lite", "strict", "ultra", "super-ultra":
		return raw
	default:
		// super-ultra is the intentional default — see DECISIONS.md.
		return "super-ultra"
	}
}

// isOutputEnabled returns whether output compression is enabled.
// Defaults to true if not explicitly set to false.
//
// Purpose: Check if output compression subcommands should emit anything.
// Errors: Never errors.
func isOutputEnabled() bool {
	s := loadSettings()
	if s.Pakka.Compress.Output == nil {
		return true
	}
	return *s.Pakka.Compress.Output
}
