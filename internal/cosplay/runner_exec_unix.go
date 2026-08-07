//go:build !windows

package cosplay

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup places the child in its own process group so a
// timeout kill can take down the whole tree (go run compiles a binary that
// outlives the `go run` wrapper; python can spawn grandchildren).
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree kills the child's entire process group.
func killProcessTree(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		// Negative pid = the process group (Setpgid made the child a leader).
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
