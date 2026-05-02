package main

import "testing"

func TestStackGateCmdName(t *testing.T) {
	cmd := &StackGateCmd{}
	if cmd.Name() != "stack-gate" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "stack-gate")
	}
}

func TestStackGateCmdImplementsCommand(t *testing.T) {
	var _ Command = &StackGateCmd{}
}
