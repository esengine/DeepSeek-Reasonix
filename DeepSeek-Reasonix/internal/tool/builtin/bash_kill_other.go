//go:build !windows

package builtin

import (
	"os/exec"
	"syscall"
)

// setKillTree makes a cancelled command kill its whole process group, not just
// the shell leader — otherwise `go test ./...` and the test binaries it spawns
// outlive an Esc. The child gets its own group (Setpgid) so the negative-pid
// signal reaches every descendant.
func setKillTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
