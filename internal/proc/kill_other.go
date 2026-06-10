//go:build !windows

package proc

import "os/exec"

// KillTree terminates cmd's process; off Windows it kills the direct child only.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

// StartTracked starts cmd. Off Windows there is no Job Object to track it in —
// the platform reaps the child directly — so it just starts and returns a 0
// handle, and KillTracked falls back to KillTree.
func StartTracked(cmd *exec.Cmd) (uintptr, error) {
	return 0, cmd.Start()
}

// KillTracked terminates cmd's process tree; the handle is unused off Windows.
func KillTracked(cmd *exec.Cmd, _ uintptr) { KillTree(cmd) }
