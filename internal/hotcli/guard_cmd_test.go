package hotcli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGuardCmdName(t *testing.T) {
	cmd := &GuardCmd{}
	if cmd.Name() != "guard" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "guard")
	}
}

func TestGuardCmdImplementsCommand(t *testing.T) {
	var _ Command = &GuardCmd{}
}

func TestAskDecisionJSONShape(t *testing.T) {
	out := askDecisionJSON("blocked: directory traversal")
	var parsed struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("askDecisionJSON not valid JSON: %v", err)
	}
	if parsed.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q", parsed.HookSpecificOutput.HookEventName)
	}
	if parsed.HookSpecificOutput.PermissionDecision != "ask" {
		t.Errorf("permissionDecision = %q, want ask", parsed.HookSpecificOutput.PermissionDecision)
	}
	if !strings.Contains(parsed.HookSpecificOutput.PermissionDecisionReason, "directory traversal") {
		t.Errorf("reason should carry the block reason: %q", parsed.HookSpecificOutput.PermissionDecisionReason)
	}
	if !strings.Contains(parsed.HookSpecificOutput.PermissionDecisionReason, "guard-allowlist.json") {
		t.Errorf("reason should explain the override mechanism: %q", parsed.HookSpecificOutput.PermissionDecisionReason)
	}
}

func TestLoadGuardConfigDefaults(t *testing.T) {
	cfg := loadGuardConfig()
	if cfg.DemoteThreshold <= 0 || cfg.DecayWindowDays <= 0 {
		t.Errorf("guard config must have positive defaults: %+v", cfg)
	}
}
