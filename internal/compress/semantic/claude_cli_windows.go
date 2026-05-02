//go:build windows

package semantic

import "os/exec"

// init: Windows has no Setpgid. The default Cancel = Process.Kill is
// adequate for the production case (claude.exe is a single binary, no shell
// children to chase). The unit-test fixture skips on Windows for the same
// reason — see claude_cli_test.go.
func init() {
	configureProcessGroup = func(_ *exec.Cmd) {}
}
