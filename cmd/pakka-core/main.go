// Package main provides the pakka-core CLI.
//
// pakka-core is the single binary invoked by all pakka hooks and skills.
// Subcommands are added incrementally across build passes (see DESIGN.md §10):
//
//	Pass 1: status-line, audit
//	Pass 2: compress, meter
//	Pass 3: guard, install-git-hook
//	Pass 3.1: commit-gate, help
//	Pass 4: stack-detect, stack-gate, eval
//	Pass 5: report
//	Pass 5b: bench
package main

import (
	"fmt"
	"os"
)

const version = "0.3.0"


func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "pakka-core %s — no subcommand\n", version)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "status-line":
		_ = (&StatusLineCmd{}).Run(os.Args[2:])
	case "audit":
		_ = (&AuditCmd{}).Run(os.Args[2:])
	case "compress":
		_ = (&CompressCmd{}).Run(os.Args[2:])
	case "meter":
		_ = (&MeterCmd{}).Run(os.Args[2:])
	case "guard":
		_ = (&GuardCmd{}).Run(os.Args[2:])
	case "commit-gate":
		_ = (&CommitGateCmd{}).Run(os.Args[2:])
	case "help":
		_ = (&HelpCmd{}).Run(os.Args[2:])
	case "install-git-hook":
		_ = (&InstallGitHookCmd{}).Run(os.Args[2:])
	case "stack-detect":
		_ = (&StackDetectCmd{}).Run(os.Args[2:])
	case "stack-gate":
		_ = (&StackGateCmd{}).Run(os.Args[2:])
	case "eval":
		_ = (&EvalCmd{}).Run(os.Args[2:])
	case "report":
		_ = (&ReportCmd{}).Run(os.Args[2:])
	case "bench":
		_ = (&BenchCmd{}).Run(os.Args[2:])
	case "index":
		_ = (&IndexCmd{}).Run(os.Args[2:])
	case "query":
		_ = (&QueryCmd{}).Run(os.Args[2:])
	case "output-rules":
		_ = (&OutputRulesCmd{}).Run(os.Args[2:])
	case "output-reinforce":
		_ = (&OutputReinforceCmd{}).Run(os.Args[2:])
	case "orchestrator-status":
		runOrchestratorStatus()
	default:
		fmt.Fprintf(os.Stderr, "pakka-core %s — unknown subcommand %q\n", version, os.Args[1])
		os.Exit(2)
	}
}

