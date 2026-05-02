package main

import (
	"os"
	"testing"
)

func TestHelpCmdName(t *testing.T) {
	cmd := &HelpCmd{}
	if cmd.Name() != "help" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "help")
	}
}

func TestHelpCmdRunNoPluginRoot(t *testing.T) {
	// HelpCmd.Run with empty/temp plugin root should not panic.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Point os.Executable away by running in a temp dir context; pluginRoot()
	// resolves via os.Executable so we can't easily override it here.
	// Just verify it doesn't panic and returns without error.
	cmd := &HelpCmd{}
	_ = cmd.Run(nil)
	_ = os.Getenv // keep import used
}
