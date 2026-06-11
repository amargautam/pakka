package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/audit"
	"github.com/amargautam/pakka/internal/guard"
	"github.com/amargautam/pakka/internal/hookevent"
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
	if phase == "tool-post" {
		recordGuardOverride(event)
	}
	if err := audit.Run(event, phase); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: audit: %v\n", err)
		os.Exit(1)
	}
}

// recordGuardOverride implements guard override learning (issue #12): if
// guard asked about this exact tool input and the tool ran anyway, the user
// approved — record the override in the repo's allowlist. Marker validation
// against the executed command lives in guard.RecordApprovedOverride.
func recordGuardOverride(event *hookevent.Event) {
	if event.ToolName != "Bash" || len(event.ToolInput) == 0 {
		return
	}
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(event.ToolInput, &input); err != nil {
		return
	}
	pattern, recorded, err := guard.RecordApprovedOverride(
		guard.InputHash(event.ToolInput), event.SessionID, input.Command, event.CWD,
		loadGuardConfig(), time.Now())
	switch {
	case err != nil:
		_ = audit.WriteNote(event.SessionID, "guard_override_error", err.Error())
	case recorded:
		_ = audit.WriteNote(event.SessionID, "guard_override", "override_recorded pattern="+pattern)
	}
}
