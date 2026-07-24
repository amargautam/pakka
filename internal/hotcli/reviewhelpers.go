package hotcli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"strings"
)

// These review/diff helpers are shared between the hot-path commit-gate (this
// package) and the review-pass command (internal/cli, via shim.go). They live
// here so the hot binary owns them without importing cli, and cli reuses them
// without duplicating the git/hash logic.

// extractRationales concatenates the "rationale" text of every parsed finding
// row into a single space-separated string, so the audit "review-verdict"
// entry carries the findings' prose for FTS5 to match.
func extractRationales(data []byte) string {
	var parts []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Rationale string `json:"rationale"`
		}
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		if row.Rationale != "" {
			parts = append(parts, row.Rationale)
		}
	}
	return strings.Join(parts, " ")
}

// stagedDiff returns the raw bytes of `git diff --cached` for the repo rooted
// at root (or the process CWD repo when root is ""). The bytes are hashed
// verbatim, so both writer and gate must invoke git identically.
func stagedDiff(root string) ([]byte, error) {
	args := []string{}
	if root != "" {
		args = append(args, "-C", root)
	}
	args = append(args, "diff", "--cached")
	return exec.Command("git", args...).Output()
}

// sha256Hex returns the lowercase hex sha256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
