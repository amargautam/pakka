package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/amargautam/pakka/internal/compress/orchestrator"
	"github.com/amargautam/pakka/internal/hookevent"
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

// parseStatusLineInput reads the statusLine stdin JSON once and parses it as
// both a lenient hook event (legacy fields: cwd, transcript_path, session_id)
// and a CC 2.1 native payload (cost, context_window). native is nil on older
// Claude Code payloads or malformed input → statusline falls back to its
// transcript-scan path.
func parseStatusLineInput(r io.Reader) (*hookevent.Event, *statusline.NativePayload) {
	raw, _ := io.ReadAll(r)
	event := parseLenient(bytes.NewReader(raw))
	return event, statusline.ParseNativePayload(raw)
}

func runStatusLine() {
	event, native := parseStatusLineInput(os.Stdin)
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
	if err := statusline.Run(event, native, os.Stdout, level, stale); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: status-line: %v\n", err)
		os.Exit(1)
	}
}
