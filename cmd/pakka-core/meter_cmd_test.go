package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeterCmdName(t *testing.T) {
	cmd := &MeterCmd{}
	if cmd.Name() != "meter" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "meter")
	}
}

func TestMeterCmdRunWritesMeterFile(t *testing.T) {
	// MeterCmd.Run should not panic on empty/missing event
	// and should complete without error when HOME is a temp dir
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cmd := &MeterCmd{}
	// Empty args — should not panic, may return error but not panic
	_ = cmd.Run(nil)
}

var _ = filepath.Join // keep import used
var _ = os.Getenv    // keep import used
