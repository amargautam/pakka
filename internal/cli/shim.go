package cli

// shim.go re-exposes the shared helpers that moved to internal/hotcli under the
// unexported names cli used before the hot/cold binary split, so none of cli's
// existing call sites had to change. hotcli owns the single implementation of
// each; these are thin delegators. See internal/hotcli/exports.go and
// docs/specs/2026-07-24-hot-path-startup-floor.md.

import (
	"io"

	"github.com/amargautam/pakka/internal/commitgate"
	"github.com/amargautam/pakka/internal/guard"
	"github.com/amargautam/pakka/internal/hookevent"
	"github.com/amargautam/pakka/internal/hotcli"
)

// settingsJSON is the parsed pakka section of settings.json (owned by hotcli).
type settingsJSON = hotcli.Settings

func loadSettings() settingsJSON                                    { return hotcli.LoadSettings() }
func pluginRoot() string                                            { return hotcli.PluginRoot() }
func parseStrict(r io.Reader, w io.Writer) (*hookevent.Event, bool) { return hotcli.ParseStrict(r, w) }
func parseLenient(r io.Reader) *hookevent.Event                     { return hotcli.ParseLenient(r) }
func debugLogf(format string, args ...interface{})                  { hotcli.DebugLogf(format, args...) }
func resolveOutputLevel(raw string) string                          { return hotcli.ResolveOutputLevel(raw) }
func loadOutputLevel() string                                       { return hotcli.LoadOutputLevel() }
func isOutputEnabled() bool                                         { return hotcli.IsOutputEnabled() }
func repoRoot() string                                              { return hotcli.RepoRoot() }
func repoRootAt(dir string) string                                  { return hotcli.RepoRootAt(dir) }
func resolveReviewsDir(cmd string) string                           { return hotcli.ResolveReviewsDir(cmd) }
func sha256Hex(b []byte) string                                     { return hotcli.Sha256Hex(b) }
func stagedDiff(root string) ([]byte, error)                        { return hotcli.StagedDiff(root) }
func extractRationales(data []byte) string                          { return hotcli.ExtractRationales(data) }
func loadGuardConfig() guard.Config                                 { return hotcli.LoadGuardConfig() }

func gatherReviewState(cfg *commitgate.Config, cmd string) (*commitgate.State, *commitgate.ReviewVerdict) {
	return hotcli.GatherReviewState(cfg, cmd)
}

func maybeWriteReviewVerdict(sessionID string, d *commitgate.Decision, verdict *commitgate.ReviewVerdict) {
	hotcli.MaybeWriteReviewVerdict(sessionID, d, verdict)
}
