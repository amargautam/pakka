package main

import (
	"fmt"
	"os"

	"github.com/amargautam/pakka/internal/stackgate"
)

// StackGateCmd implements the "stack-gate" subcommand.
type StackGateCmd struct{}

func (c *StackGateCmd) Name() string { return "stack-gate" }
func (c *StackGateCmd) Run(args []string) error {
	runStackGate()
	return nil
}

// --- stack-gate (Pass 4) ---

func runStackGate() {
	event, ok := parseStrict(os.Stdin, os.Stderr)
	if !ok {
		os.Exit(1)
	}
	if event == nil {
		return // empty stdin — silent skip
	}

	// Determine project directory
	cwd := event.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Load .pakka/stack.json — if missing, exit 0 silently
	cfg := stackgate.LoadConfig(cwd)
	if cfg == nil {
		return
	}

	result := stackgate.Run(event, cfg)
	if !result.Passed {
		fmt.Fprint(os.Stderr, result.Output)
		os.Exit(2)
	}
}
