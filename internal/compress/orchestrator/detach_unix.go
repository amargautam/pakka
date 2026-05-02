//go:build !windows

package orchestrator

import (
	"os/exec"
	"syscall"
)

// applyDetach configures cmd so the child process survives the parent's exit
// and does not inherit its controlling terminal. Setsid puts the child in a
// new session.
func applyDetach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
