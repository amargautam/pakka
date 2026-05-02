package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// OutputReinforceCmd implements the "output-reinforce" subcommand.
type OutputReinforceCmd struct{}

func (c *OutputReinforceCmd) Name() string { return "output-reinforce" }
func (c *OutputReinforceCmd) Run(args []string) error {
	runOutputReinforce()
	return nil
}

// --- output-reinforce (Pass 4.1) ---

// runOutputReinforce emits a short per-turn reinforcement JSON to stdout.
// Used by UserPromptSubmit hook to prevent drift after many turns.
//
// Purpose: Reinforce output compression rules on every user prompt.
// Errors: None; always succeeds or emits nothing.
func runOutputReinforce() {
	if !isOutputEnabled() {
		return
	}

	level := loadOutputLevel()

	reinforce := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": fmt.Sprintf("PAKKA OUTPUT COMPRESSION ACTIVE (%s). Drop articles/filler/pleasantries/hedging. Fragments OK. Code/commits/security: write normal.", level),
		},
	}

	_ = json.NewEncoder(os.Stdout).Encode(reinforce)
}
