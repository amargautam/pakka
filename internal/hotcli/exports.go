// Package hotcli holds the pakka hot-path subcommands — guard, commit-gate, and
// status-line — plus the git/settings/parse helpers they share with the rest of
// the CLI. It exists as a separate package so the lean pakka-hot binary
// (cmd/pakka-hot) can link ONLY these three commands, keeping two heavy
// dependencies out of the hot binary:
//
//   - internal/recall → modernc.org/sqlite (the netdb package-init floor), and
//   - internal/compress/semantic → net/http (the Anthropic client init).
//
// Neither is reachable from this package. internal/cli (the fat pakka-core
// side) imports hotcli and re-exposes the shared helpers through unexported
// shims (see internal/cli/shim.go), so cli's existing call sites are unchanged
// and there is a single implementation of each helper.
//
// See docs/specs/2026-07-24-hot-path-startup-floor.md.
package hotcli

import (
	"fmt"
	"io"
	"os"

	"github.com/amargautam/pakka/internal/commitgate"
	"github.com/amargautam/pakka/internal/guard"
	"github.com/amargautam/pakka/internal/hookevent"
)

const version = "0.5.0"

// DispatchHot routes the three hot-path subcommands served by pakka-hot.
// argv is os.Args. Returns the process exit code. The individual command Run
// methods call os.Exit directly on block/error, so this returns 0 on the
// allow/no-op paths and 2 only for a missing/unknown subcommand.
func DispatchHot(argv []string) int {
	if len(argv) < 2 {
		fmt.Fprintf(os.Stderr, "pakka-hot %s — no subcommand\n", version)
		return 2
	}
	switch argv[1] {
	case "guard":
		_ = (&GuardCmd{}).Run(argv[2:])
	case "commit-gate":
		_ = (&CommitGateCmd{}).Run(argv[2:])
	case "status-line":
		_ = (&StatusLineCmd{}).Run(argv[2:])
	default:
		fmt.Fprintf(os.Stderr, "pakka-hot %s — unknown or non-hot subcommand %q (hot binary serves guard, commit-gate, status-line)\n", version, argv[1])
		return 2
	}
	return 0
}

// --- exported wrappers over the shared helpers, consumed by internal/cli ------
// Kept as a single seam so internal/cli/shim.go can re-expose them under the
// package's historical unexported names without touching call sites.

// Settings is the parsed pakka section of settings.json.
type Settings = settingsJSON

// LoadSettings reads and parses settings.json from the plugin root.
func LoadSettings() settingsJSON { return loadSettings() }

// PluginRoot returns the plugin root (two levels above the running executable).
func PluginRoot() string { return pluginRoot() }

// ParseStrict parses a hook event with hard-fail semantics (guard/commit-gate).
func ParseStrict(r io.Reader, w io.Writer) (*hookevent.Event, bool) { return parseStrict(r, w) }

// ParseLenient parses a hook event, synthesizing a SessionID when absent.
func ParseLenient(r io.Reader) *hookevent.Event { return parseLenient(r) }

// DebugLogf appends a timestamped line to ~/.pakka/debug.log.
func DebugLogf(format string, args ...interface{}) { debugLogf(format, args...) }

// ResolveOutputLevel applies the level-defaulting policy to a raw string.
func ResolveOutputLevel(raw string) string { return resolveOutputLevel(raw) }

// LoadOutputLevel returns the configured output-compression level.
func LoadOutputLevel() string { return loadOutputLevel() }

// IsOutputEnabled reports whether output compression is enabled.
func IsOutputEnabled() bool { return isOutputEnabled() }

// RepoRoot returns the git repo root for the process CWD, or "".
func RepoRoot() string { return repoRoot() }

// RepoRootAt returns the git repo root for dir, or "".
func RepoRootAt(dir string) string { return repoRootAt(dir) }

// ResolveReviewsDir returns the .pakka/reviews dir for the repo a git command targets.
func ResolveReviewsDir(cmd string) string { return resolveReviewsDir(cmd) }

// Sha256Hex returns the lowercase hex sha256 of b.
func Sha256Hex(b []byte) string { return sha256Hex(b) }

// StagedDiff returns the raw bytes of `git diff --cached` for root.
func StagedDiff(root string) ([]byte, error) { return stagedDiff(root) }

// ExtractRationales concatenates finding rationales for the audit trail.
func ExtractRationales(data []byte) string { return extractRationales(data) }

// LoadGuardConfig reads guard thresholds from settings.json.
func LoadGuardConfig() guard.Config { return loadGuardConfig() }

// GatherReviewState resolves the gate's view of review state for a commit command.
func GatherReviewState(cfg *commitgate.Config, cmd string) (*commitgate.State, *commitgate.ReviewVerdict) {
	return gatherReviewState(cfg, cmd)
}

// MaybeWriteReviewVerdict appends the "review-verdict" audit entry for an
// authorized findings-bound commit. Exposed for the review-provenance
// integration test that lives in internal/cli.
func MaybeWriteReviewVerdict(sessionID string, d *commitgate.Decision, verdict *commitgate.ReviewVerdict) {
	maybeWriteReviewVerdict(sessionID, d, verdict)
}
