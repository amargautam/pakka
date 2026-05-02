package main

import (
	"fmt"
	"os"

	"github.com/amargautam/pakka/internal/meter"
)

// MeterCmd implements the "meter" subcommand.
type MeterCmd struct{}

func (c *MeterCmd) Name() string { return "meter" }
func (c *MeterCmd) Run(args []string) error {
	runMeter()
	return nil
}

func runMeter() {
	event := parseLenient(os.Stdin)
	if err := meter.Run(event); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: meter: %v\n", err)
		os.Exit(1)
	}
}
