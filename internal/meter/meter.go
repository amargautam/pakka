// Package meter writes token-usage JSONL entries for Claude Code sessions.
//
// Each entry records tokens consumed (from tool usage) or bytes saved
// (from compression) with a derived token estimate. The status-line
// reads these to report cumulative-per-repo totals.
package meter

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/data"
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

	return data.AppendJSONL(path, entry)
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

	return data.AppendJSONL(path, entry)
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

// estimateTokens approximates token count from event payload sizes.
// Uses 3.5 bytes/token — consistent with WriteSavings calibration.
func estimateTokens(event *hookevent.Event) int64 {
	n := float64(len(event.ToolInput)) + float64(len(event.ToolResponse))
	return int64(math.Round(n / 3.5))
}

func shortSID(sid string) string {
	var b strings.Builder
	for _, r := range sid {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	clean := b.String()
	if len(clean) > 8 {
		return clean[:8]
	}
	return clean
}
