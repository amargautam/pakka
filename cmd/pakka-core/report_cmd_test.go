package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReportCmdName(t *testing.T) {
	cmd := &ReportCmd{}
	if cmd.Name() != "report" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "report")
	}
}

func TestReportCmdImplementsCommand(t *testing.T) {
	var _ Command = &ReportCmd{}
}

func TestReportCmdRunWithEmptyDirs(t *testing.T) {
	// ReportCmd.Run should not panic when meter/audit dirs exist but are empty.
	// runReport calls os.Exit on missing dirs, so pre-create them.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	_ = os.MkdirAll(filepath.Join(tmp, ".pakka", "meter"), 0755)
	_ = os.MkdirAll(filepath.Join(tmp, ".pakka", "audit"), 0755)
	cmd := &ReportCmd{}
	// Empty dirs → Gather returns no error, report is printed. Should not panic.
	_ = cmd.Run(nil)
}
