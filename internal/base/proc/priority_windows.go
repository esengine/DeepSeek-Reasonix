//go:build windows

package proc

import (
	"os/exec"
	"syscall"
)

const belowNormalPriorityClass = 0x00004000 // BELOW_NORMAL_PRIORITY_CLASS

// LowPriority marks a not-yet-started command to run below normal priority,
// for background helpers (indexers) that must not starve the user's machine.
func LowPriority(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= belowNormalPriorityClass
}

// LowPriorityStarted is a no-op on Windows; priority is set at creation.
func LowPriorityStarted(*exec.Cmd) {}
