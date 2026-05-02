package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/amargautam/pakka/internal/audit"
)

// AuditCmd implements the "audit" subcommand.
type AuditCmd struct{}

func (c *AuditCmd) Name() string { return "audit" }
func (c *AuditCmd) Run(args []string) error {
	runAudit()
	return nil
}

func runAudit() {
	phase := "tool-post"
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--phase=") {
			phase = strings.TrimPrefix(a, "--phase=")
		}
	}
	event := parseLenient(os.Stdin)
	if err := audit.Run(event, phase); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: audit: %v\n", err)
		os.Exit(1)
	}
}
