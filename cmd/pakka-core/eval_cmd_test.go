package main

import "testing"

func TestEvalCmdName(t *testing.T) {
	cmd := &EvalCmd{}
	if cmd.Name() != "eval" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "eval")
	}
}

func TestEvalCmdImplementsCommand(t *testing.T) {
	var _ Command = &EvalCmd{}
}
