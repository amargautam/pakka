//go:build !windows

package semantic

import (
	"os/exec"
	"syscall"
)

// init registers the POSIX process-group configurator. We put the subprocess
// into its own group via Setpgid; on context cancel we send SIGKILL to the
// negative pid (the whole group) so any descendants — typically a `sh -c`
// chain that exec'd `sleep` — die together. Without this the test fixture
// in claude_cli_test.go's TestClaudeCLI_TimeoutKillsSubprocess would
// outlast the timeout while the orphaned `sleep` runs to completion.
func init() {
	configureProcessGroup = func(cmd *exec.Cmd) {
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.Setpgid = true

		oldCancel := cmd.Cancel
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				// Negative pid → kill entire process group.
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			if oldCancel != nil {
				return oldCancel()
			}
			return nil
		}
	}
}
