package main

import (
	"encoding/json"
	"os"

	"github.com/amargautam/pakka/internal/stackdetect"
)

// StackDetectCmd implements the "stack-detect" subcommand.
type StackDetectCmd struct{}

func (c *StackDetectCmd) Name() string { return "stack-detect" }
func (c *StackDetectCmd) Run(args []string) error {
	runStackDetect()
	return nil
}

// --- stack-detect (Pass 4) ---

func runStackDetect() {
	// Try to get CWD from event JSON on stdin, fall back to os.Getwd().
	cwd := ""
	event := parseLenient(os.Stdin)
	if event.CWD != "" {
		cwd = event.CWD
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	result := stackdetect.Detect(cwd)
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
