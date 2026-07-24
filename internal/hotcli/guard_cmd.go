package hotcli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

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

// loadGuardConfig reads allowlist thresholds from settings.json
// (pakka.guard.demoteThreshold, pakka.guard.decayWindowDays).
func loadGuardConfig() guard.Config {
	cfg := guard.DefaultConfig()
	s := loadSettings()
	if v := s.Pakka.Guard.DemoteThreshold; v != nil && *v > 0 {
		cfg.DemoteThreshold = *v
	}
	if v := s.Pakka.Guard.DecayWindowDays; v != nil && *v > 0 {
		cfg.DecayWindowDays = *v
	}
	return cfg
}

func runGuard() {
	event, ok := parseStrict(os.Stdin, os.Stderr)
	if !ok {
		os.Exit(1)
	}
	if event == nil {
		return // empty stdin — silent skip
	}
	result := guard.RunWithConfig(event, loadGuardConfig())

	if result.AllowlistErr != "" {
		_ = audit.WriteNote(event.SessionID, "guard_allowlist_error", result.AllowlistErr)
	}
	if result.PolicyLocked != "" {
		_ = audit.WriteNote(event.SessionID, "policy-clamp", "guard_category_locked="+result.PolicyLocked)
	}
	if result.PolicyErr != "" {
		_ = audit.WriteNote(event.SessionID, "guard_policy_error", result.PolicyErr)
	}

	if result.Allowed {
		switch {
		case result.AllowlistedBy != "":
			_ = audit.WriteNote(event.SessionID, "guard_allow", "guard_allowlisted="+result.AllowlistedBy)
		case result.Warned:
			_ = audit.WriteNote(event.SessionID, "guard_warn", "guard_warn="+result.Pattern)
			fmt.Fprintf(os.Stderr, "pakka guard: warn — pattern %q demoted by repeated overrides (%s)\n", result.Pattern, result.Reason)
		}
		return
	}

	_ = audit.RunBlock(event, result.Reason)

	if result.Allowlistable && result.RepoRoot != "" {
		// Overridable block: ask the user instead of hard-denying. If they
		// approve, the tool runs and the PostToolUse audit hook consumes the
		// pending marker to record the override in the repo allowlist.
		_ = guard.WritePendingOverride(guard.InputHash(event.ToolInput), guard.PendingOverride{
			Pattern:   result.Pattern,
			Shape:     result.Shape,
			Root:      result.RepoRoot,
			SessionID: event.SessionID,
		}, time.Now())
		fmt.Println(askDecisionJSON(result.Reason))
		return
	}

	fmt.Fprintf(os.Stderr, "pakka guard: %s\n", result.Reason)
	os.Exit(2)
}

// askDecisionJSON builds the PreToolUse hook JSON that surfaces the block as
// a user permission prompt instead of a hard deny.
func askDecisionJSON(reason string) string {
	out := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":      "PreToolUse",
			"permissionDecision": "ask",
			"permissionDecisionReason": fmt.Sprintf(
				"pakka guard: %s — approving records a per-repo override in .pakka/guard-allowlist.json; repeated overrides demote this pattern to warn", reason),
		},
	}
	b, _ := json.Marshal(out)
	return string(b)
}
