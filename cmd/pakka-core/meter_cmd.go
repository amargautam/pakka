package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/amargautam/pakka/internal/hookevent"
	"github.com/amargautam/pakka/internal/meter"
	"github.com/amargautam/pakka/internal/statusline"
)

// MeterCmd implements the "meter" subcommand.
type MeterCmd struct{}

func (c *MeterCmd) Name() string { return "meter" }
func (c *MeterCmd) Run(args []string) error {
	runMeter()
	return nil
}

func runMeter() {
	phase := ""
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--phase=") {
			phase = strings.TrimPrefix(a, "--phase=")
		}
	}

	event := parseLenient(os.Stdin)

	if phase == "session-end" {
		runMeterSessionEnd(event)
		return
	}

	if err := meter.Run(event); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: meter: %v\n", err)
		os.Exit(1)
	}
}

// runMeterSessionEnd records the assistant output tokens observed in the
// session's Claude Code transcripts into ~/.pakka/meter/<sid>.jsonl.
//
// Degrades gracefully: if transcripts are unreadable mid-session, writes 0
// rather than failing the hook. The SessionEnd hook must not block.
func runMeterSessionEnd(event *hookevent.Event) {
	repo := sessionRepoRoot(event.CWD)

	var outTokens int64
	if n, err := statusline.RepoOutputTokens("", repo); err == nil {
		outTokens = n
	}

	if err := meter.WriteSessionEnd(event.SessionID, repo, outTokens); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: meter session-end: %v\n", err)
		// Non-fatal: don't block SessionEnd.
	}
}

// sessionRepoRoot resolves the canonical repo_root tag for a session-end
// snapshot: git toplevel of the session cwd, symlink-resolved; the
// canonicalized cwd itself when not inside a git repo (multi-repo workspace
// dirs). Falls back to the process cwd when the hook event carries none so
// every session-end snapshot gets a non-empty, consistent tag.
func sessionRepoRoot(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	return meter.RepoKey(cwd)
}
