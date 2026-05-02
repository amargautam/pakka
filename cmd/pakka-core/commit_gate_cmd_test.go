package main

import "testing"

func TestCommitGateCmdName(t *testing.T) {
	cmd := &CommitGateCmd{}
	if cmd.Name() != "commit-gate" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "commit-gate")
	}
}

func TestCommitGateCmdImplementsCommand(t *testing.T) {
	var _ Command = &CommitGateCmd{}
}
