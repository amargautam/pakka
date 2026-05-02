package main

import "testing"

func TestAuditCmdName(t *testing.T) {
	cmd := &AuditCmd{}
	if cmd.Name() != "audit" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "audit")
	}
}

func TestAuditCmdImplementsCommand(t *testing.T) {
	var _ Command = &AuditCmd{}
}
