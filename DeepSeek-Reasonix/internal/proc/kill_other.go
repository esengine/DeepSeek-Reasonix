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

// TrackTree is a no-op off Windows (returns 0); KillTracked then falls back to
// KillTree, which is sufficient where the platform reaps the child directly.
func TrackTree(_ *exec.Cmd) uintptr { return 0 }

// KillTracked terminates cmd's process tree; the handle is unused off Windows.
func KillTracked(cmd *exec.Cmd, _ uintptr) { KillTree(cmd) }
