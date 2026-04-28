// Package meter writes token-usage JSONL entries for Claude Code sessions.
//
// Each entry records tokens consumed (from tool usage) or bytes saved
// (from compression) with a derived token estimate. The status-line
// reads these to report cumulative-per-repo totals.
package meter

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/hookevent"
)

// Entry is one line in the meter JSONL file.
//
// Repo is the canonical absolute path of the repo this entry was produced
// from (git toplevel of cwd, or cwd if not a git repo). Empty on legacy
// entries written before the repo tag was introduced — readers must skip
// or bucket them under no-repo.
type Entry struct {
	TS             string `json:"ts"`
	SessionID      string `json:"session_id"`
	Repo           string `json:"repo,omitempty"`
	TokensUsed     int64  `json:"tokens_used"`
	BytesSaved     int64  `json:"bytes_saved"`
	TokensSavedEst int64  `json:"tokens_saved_est"`
	OutputTokens   int64  `json:"output_tokens,omitempty"`
}

// RepoKey returns the canonical repo identifier for a working directory.
//
// Purpose: Tag meter entries and filter status-line aggregates by repo.
// Strategy: `git rev-parse --show-toplevel`; fall back to the absolute form
// of cwd when not a git repo. Returns "" only if cwd resolves to "".
// Errors: Never errors; always returns a string suitable as a tag.
func RepoKey(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	// Try git toplevel. Fast path; failure → fallback.
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err == nil {
		top := strings.TrimSpace(string(out))
		if top != "" {
			if abs, err := filepath.Abs(top); err == nil {
				return abs
			}
			return top
		}
	}
	// Fallback: absolute cwd.
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs
	}
	return cwd
}

// Run appends a token-usage entry for the given hook event.
//
// Purpose: Track per-tool-use token consumption in ~/.pakka/meter/<session>.jsonl.
// Errors: Returns error on filesystem failures (mkdir, open, write).
func Run(event *hookevent.Event) error {
	dir, err := meterDir()
	if err != nil {
		return err
	}

	sid := shortSID(event.SessionID)
	path := filepath.Join(dir, sid+".jsonl")

	entry := Entry{
		TS:         time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:  event.SessionID,
		Repo:       RepoKey(event.CWD),
		TokensUsed: estimateTokens(event),
	}

	return appendEntry(path, entry)
}

// WriteSavings appends a compression-savings entry for the given session.
//
// Purpose: Record bytes saved by compression with a derived token estimate.
// Errors: Returns error on filesystem failures.
func WriteSavings(sessionID, repo string, bytesSaved int64) error {
	dir, err := meterDir()
	if err != nil {
		return err
	}

	sid := shortSID(sessionID)
	path := filepath.Join(dir, sid+".jsonl")

	entry := Entry{
		TS:             time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:      sessionID,
		Repo:           repo,
		BytesSaved:     bytesSaved,
		TokensSavedEst: int64(math.Round(float64(bytesSaved) / 3.5)),
	}

	return appendEntry(path, entry)
}

// WriteOutputTokens appends an entry recording assistant output tokens
// for the given session.
//
// Purpose: Record output tokens read from a Claude Code transcript so the
// status-line can compute output savings.
// Errors: Returns error on filesystem failures.
func WriteOutputTokens(sessionID, repo string, outputTokens int64) error {
	dir, err := meterDir()
	if err != nil {
		return err
	}

	sid := shortSID(sessionID)
	path := filepath.Join(dir, sid+".jsonl")

	entry := Entry{
		TS:           time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:    sessionID,
		Repo:         repo,
		OutputTokens: outputTokens,
	}

	return appendEntry(path, entry)
}

func meterDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".pakka", "meter")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func appendEntry(path string, entry Entry) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// estimateTokens approximates token count from event payload sizes.
// Rough heuristic: 1 token ≈ 4 characters.
func estimateTokens(event *hookevent.Event) int64 {
	n := int64(len(event.ToolInput)) + int64(len(event.ToolResponse))
	return n / 4
}

func shortSID(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}
