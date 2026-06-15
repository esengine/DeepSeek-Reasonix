//go:build !windows

package control

import (
	"os/exec"
	"syscall"
)

// setShellKillTree makes cancellation kill the whole shell tree. Running the
// child in a new session also keeps interactive prompts from grabbing the TUI's
// controlling terminal.
func setShellKillTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
