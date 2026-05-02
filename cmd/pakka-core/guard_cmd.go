package main

import (
	"fmt"
	"os"

	"github.com/amargautam/pakka/internal/audit"
	"github.com/amargautam/pakka/internal/guard"
)

// GuardCmd implements the "guard" subcommand.
type GuardCmd struct{}

func (c *GuardCmd) Name() string { return "guard" }
func (c *GuardCmd) Run(args []string) error {
	runGuard()
	return nil
}

func runGuard() {
	event, ok := parseStrict(os.Stdin, os.Stderr)
	if !ok {
		os.Exit(1)
	}
	if event == nil {
		return // empty stdin — silent skip
	}
	result := guard.Run(event)
	if !result.Allowed {
		_ = audit.RunBlock(event, result.Reason)
		fmt.Fprintf(os.Stderr, "pakka guard: %s\n", result.Reason)
		os.Exit(2)
	}
}
