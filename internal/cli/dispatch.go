// Dispatch is the shared subcommand router for the pakka binaries.
//
// Two binaries link this package:
//
//   - pakka-core (cmd/pakka-core) — the full binary. It wires IndexFunc and
//     QueryFunc to the recall implementation (which links modernc.org/sqlite)
//     and calls Dispatch, so every subcommand is served.
//   - pakka-hot  (cmd/pakka-hot)  — the lean hot-path binary. It calls
//     DispatchHot, which routes only the three PreToolUse/statusLine hooks
//     (guard, commit-gate, status-line). It never references recall, so the
//     sqlite dependency (and its ~4ms netdb package-init floor) is not linked.
//
// Keeping recall behind the IndexFunc/QueryFunc function hooks — rather than a
// direct import in this package — is what lets pakka-hot import cli without
// pulling sqlite in transitively. See docs/specs/2026-07-24-hot-path-startup-floor.md.
package cli

import (
	"fmt"
	"os"

	"github.com/amargautam/pakka/internal/hotcli"
)

const version = "0.5.0"

// IndexFunc and QueryFunc are the recall subcommand handlers. pakka-core's main
// sets them; pakka-hot leaves them nil so recall (and sqlite) never link. A nil
// handler makes the subcommand behave as "unknown".
var (
	IndexFunc func(args []string) error
	QueryFunc func(args []string) error
)

// Dispatch routes the full pakka-core subcommand set. argv is os.Args.
// Returns the process exit code.
func Dispatch(argv []string) int {
	if len(argv) < 2 {
		fmt.Fprintf(os.Stderr, "pakka-core %s — no subcommand\n", version)
		return 2
	}
	rest := argv[2:]
	switch argv[1] {
	case "status-line":
		_ = (&hotcli.StatusLineCmd{}).Run(rest)
	case "audit":
		_ = (&AuditCmd{}).Run(rest)
	case "compress":
		_ = (&CompressCmd{}).Run(rest)
	case "meter":
		_ = (&MeterCmd{}).Run(rest)
	case "guard":
		_ = (&hotcli.GuardCmd{}).Run(rest)
	case "commit-gate":
		_ = (&hotcli.CommitGateCmd{}).Run(rest)
	case "review-pass":
		_ = (&ReviewPassCmd{}).Run(rest)
	case "help":
		_ = (&HelpCmd{}).Run(rest)
	case "install-git-hook":
		_ = (&InstallGitHookCmd{}).Run(rest)
	case "stack-detect":
		_ = (&StackDetectCmd{}).Run(rest)
	case "stack-gate":
		_ = (&StackGateCmd{}).Run(rest)
	case "eval":
		_ = (&EvalCmd{}).Run(rest)
	case "report":
		_ = (&ReportCmd{}).Run(rest)
	case "index":
		if IndexFunc != nil {
			_ = IndexFunc(rest)
		} else {
			return unknown(argv[1])
		}
	case "query":
		if QueryFunc != nil {
			_ = QueryFunc(rest)
		} else {
			return unknown(argv[1])
		}
	case "spec-find":
		_ = (&SpecFindCmd{}).Run(rest)
	case "spec-generate":
		_ = (&SpecGenerateCmd{}).Run(rest)
	case "output-rules":
		_ = (&OutputRulesCmd{}).Run(rest)
	case "output-reinforce":
		_ = (&OutputReinforceCmd{}).Run(rest)
	case "orchestrator-status":
		runOrchestratorStatus()
	case "backfill-output-tokens":
		_ = (&BackfillOutputTokensCmd{}).Run(rest)
	case "bench":
		_ = (&BenchCmd{}).Run(rest)
	case "calibrate":
		_ = (&CalibrateCmd{}).Run(rest)
	default:
		return unknown(argv[1])
	}
	return 0
}

func unknown(sub string) int {
	fmt.Fprintf(os.Stderr, "pakka-core %s — unknown subcommand %q\n", version, sub)
	return 2
}

// RecallEnabled reports whether pakka.recall.enabled is set (defaults true when
// the key is absent). Exposed so the recall command wiring in cmd/pakka-core can
// short-circuit without duplicating the settings-loading logic that lives here.
func RecallEnabled() bool {
	s := loadSettings()
	if s.Pakka.Recall.Enabled == nil {
		return true
	}
	return *s.Pakka.Recall.Enabled
}
