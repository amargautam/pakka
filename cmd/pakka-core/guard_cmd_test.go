package main

import "testing"

func TestGuardCmdName(t *testing.T) {
	cmd := &GuardCmd{}
	if cmd.Name() != "guard" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "guard")
	}
}

func TestGuardCmdImplementsCommand(t *testing.T) {
	var _ Command = &GuardCmd{}
}
