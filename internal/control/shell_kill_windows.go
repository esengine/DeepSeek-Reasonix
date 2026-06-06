//go:build windows

package control

import (
	"os/exec"
	"strconv"

	"reasonix/internal/proc"
)

// setShellKillTree hides the child's console and makes a cancelled command kill
// its whole process tree. Windows does not cascade a kill to child processes, so
// killing the shell leaves spawned commands running after a timeout; taskkill /T
// walks the PID tree and /F forces it.
func setShellKillTree(cmd *exec.Cmd) {
	proc.HideWindow(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
		proc.HideWindow(kill)
		_ = kill.Run()
		return cmd.Process.Kill()
	}
}
