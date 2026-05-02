package main

import "testing"

func TestInstallGitHookCmdName(t *testing.T) {
	cmd := &InstallGitHookCmd{}
	if cmd.Name() != "install-git-hook" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "install-git-hook")
	}
}

func TestInstallGitHookCmdImplementsCommand(t *testing.T) {
	var _ Command = &InstallGitHookCmd{}
}
