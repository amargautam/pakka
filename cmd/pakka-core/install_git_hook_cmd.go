package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// InstallGitHookCmd implements the "install-git-hook" subcommand.
type InstallGitHookCmd struct{}

func (c *InstallGitHookCmd) Name() string { return "install-git-hook" }
func (c *InstallGitHookCmd) Run(args []string) error {
	runInstallGitHook()
	return nil
}

// --- install-git-hook (Pass 3) ---

const prepareCommitMsgHook = `#!/bin/sh
# Installed by pakka — appends Reviewed-by-pakka trailer to human-authored commits.
# Claude Code commits are auto-signed via the PreToolUse commit-gate hook.
COMMIT_MSG_FILE="$1"
TRAILER="Reviewed-by-pakka: v0.1.0"
PASS_FILE=".pakka/reviews/last-pass-ts"
MAX_AGE=300

if [ ! -f "$PASS_FILE" ]; then exit 0; fi
PASS_TS=$(cat "$PASS_FILE" 2>/dev/null)
NOW=$(date +%s)
AGE=$(( NOW - PASS_TS ))
if [ "$AGE" -gt "$MAX_AGE" ]; then exit 0; fi
if grep -qF "$TRAILER" "$COMMIT_MSG_FILE" 2>/dev/null; then exit 0; fi
printf '\n%s\n' "$TRAILER" >> "$COMMIT_MSG_FILE"
`

func runInstallGitHook() {
	// Find git directory
	gitDir := ".git"
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "pakka: not a git repository\n")
		os.Exit(1)
	}

	hookPath := filepath.Join(gitDir, "hooks", "prepare-commit-msg")
	stateFile := filepath.Join(".pakka", "hook-installed")

	// Idempotent: check if already installed
	if _, err := os.Stat(stateFile); err == nil {
		fmt.Fprintf(os.Stderr, "pakka: git hook already installed at %s\n", hookPath)
		return
	}

	// Create hooks directory
	if err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: %v\n", err)
		os.Exit(1)
	}

	// Write hook
	if err := os.WriteFile(hookPath, []byte(prepareCommitMsgHook), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: %v\n", err)
		os.Exit(1)
	}

	// Mark installed
	if err := os.MkdirAll(".pakka", 0755); err == nil {
		_ = os.WriteFile(stateFile, []byte("installed\n"), 0644)
	}

	fmt.Fprintf(os.Stderr, "pakka: installed prepare-commit-msg hook at %s\n", hookPath)
	fmt.Fprintf(os.Stderr, "pakka: optional — for human-authored commits. Claude Code commits are auto-signed via PreToolUse.\n")
}
