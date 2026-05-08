package main

import (
	"fmt"
	"os"

	"github.com/amargautam/pakka/internal/compress/orchestrator"
	"github.com/amargautam/pakka/internal/meter"
	"github.com/amargautam/pakka/internal/statusline"
)

// StatusLineCmd implements the "status-line" subcommand.
type StatusLineCmd struct{}

func (c *StatusLineCmd) Name() string { return "status-line" }
func (c *StatusLineCmd) Run(args []string) error {
	runStatusLine()
	return nil
}

func runStatusLine() {
	event := parseLenient(os.Stdin)
	level := loadOutputLevel()

	cwd := statusline.ReadCWDFromTranscriptPath(event.TranscriptPath)
	if cwd == "" {
		cwd = event.CWD
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	event.CWD = cwd

	repoKey := meter.RepoKey(cwd)
	stale := orchestrator.CountStaleFromDisk(repoKey)
	if err := statusline.Run(event, os.Stdout, level, stale); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: status-line: %v\n", err)
		os.Exit(1)
	}
}
