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

// reapTree kills any members the command's process group left running after it
// returned — a foreground command that forked a daemon (e.g. `bazel run`'s
// server) leaves it behind, and Wait only reaped the shell leader. The group id
// is the leader's pid (Setpgid). ESRCH (empty group) is fine. See #3702.
func reapTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
