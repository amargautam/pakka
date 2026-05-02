package main

import (
	"os"
	"testing"
)

func TestStackDetectCmdName(t *testing.T) {
	cmd := &StackDetectCmd{}
	if cmd.Name() != "stack-detect" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "stack-detect")
	}
}

func TestStackDetectCmdImplementsCommand(t *testing.T) {
	var _ Command = &StackDetectCmd{}
}

func TestStackDetectCmdRunDoesNotPanic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	old, _ := os.Getwd()
	_ = os.Chdir(tmp)
	defer func() { _ = os.Chdir(old) }()

	cmd := &StackDetectCmd{}
	_ = cmd.Run(nil)
}
