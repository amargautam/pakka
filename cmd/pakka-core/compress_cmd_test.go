package main

import (
	"testing"
)

func TestCompressCmdName(t *testing.T) {
	cmd := &CompressCmd{}
	if cmd.Name() != "compress" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "compress")
	}
}

func TestCompressCmdImplementsCommand(t *testing.T) {
	var _ Command = &CompressCmd{}
}

func TestCompressCmdRunNilNoPanic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cmd := &CompressCmd{}
	// Run with nil args and no stdin data — should not panic.
	// runCompress reads os.Args so we simulate empty subcommand args.
	_ = cmd.Run(nil)
}
