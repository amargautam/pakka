package main

import "testing"

func TestOutputReinforceCmdName(t *testing.T) {
	cmd := &OutputReinforceCmd{}
	if cmd.Name() != "output-reinforce" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "output-reinforce")
	}
}

func TestOutputReinforceCmdImplementsCommand(t *testing.T) {
	var _ Command = &OutputReinforceCmd{}
}
