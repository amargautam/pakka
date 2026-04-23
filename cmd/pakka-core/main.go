// Package main provides the pakka-core CLI.
//
// pakka-core is the single binary invoked by all pakka hooks and skills.
// Subcommands are added incrementally across build passes (see DESIGN.md §10):
//
//	Pass 1: status-line, audit
//	Pass 2: compress, meter
//	Pass 3: guard
//	Pass 4: stack-detect, stack-gate, eval
//	Pass 5: report
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "pakka-core %s — no subcommand (see DESIGN.md §5.12)\n", version)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "pakka-core %s — subcommand %q not yet implemented (Pass 1)\n", version, os.Args[1])
	os.Exit(2)
}
