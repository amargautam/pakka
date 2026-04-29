//go:build windows

package orchestrator

import (
	"os/exec"
	"syscall"
)

// applyDetach configures cmd to use a new process group on Windows so the
// child survives the parent.
func applyDetach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x00000200 // CREATE_NEW_PROCESS_GROUP
}
