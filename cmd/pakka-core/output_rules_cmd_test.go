package main

import "testing"

func TestOutputRulesCmdName(t *testing.T) {
	cmd := &OutputRulesCmd{}
	if cmd.Name() != "output-rules" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "output-rules")
	}
}

func TestOutputRulesCmdImplementsCommand(t *testing.T) {
	var _ Command = &OutputRulesCmd{}
}
