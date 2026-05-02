package main

import "testing"

func TestStatusLineCmdName(t *testing.T) {
	cmd := &StatusLineCmd{}
	if cmd.Name() != "status-line" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "status-line")
	}
}

func TestStatusLineCmdImplementsCommand(t *testing.T) {
	var _ Command = &StatusLineCmd{}
}
