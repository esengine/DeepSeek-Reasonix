//go:build windows

package proc

import "os/exec"

// PrepareShellPATHProbe prepares a short-lived login-shell PATH probe process.
// Windows has no Unix controlling TTY/session job-control issue here; hide the
// helper window like other child processes started by the GUI app.
func PrepareShellPATHProbe(cmd *exec.Cmd) {
	HideWindow(cmd)
}
