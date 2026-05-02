package main

import "testing"

func TestBenchCmdName(t *testing.T) {
	cmd := &BenchCmd{}
	if cmd.Name() != "bench" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "bench")
	}
}

func TestBenchCmdImplementsCommand(t *testing.T) {
	var _ Command = &BenchCmd{}
}
