//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// LowPriority is a no-op off Windows; unix sets niceness after start.
func LowPriority(*exec.Cmd) {}

// LowPriorityStarted renices a started command to below-normal priority,
// for background helpers (indexers) that must not starve the user's machine.
func LowPriorityStarted(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
}
