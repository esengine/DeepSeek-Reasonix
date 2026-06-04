//go:build !windows

package control

import (
	"os/exec"
	"syscall"
)

// setShellKillTree makes a cancelled shell command kill its whole process group,
// not just the shell leader — otherwise a timed-out pipeline (e.g.
// !find / -name foo | grep bar) orphans the children. The child gets its own
// group (Setpgid) so the negative-pid signal reaches every descendant.
func setShellKillTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
