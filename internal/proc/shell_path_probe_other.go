//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// PrepareShellPATHProbe detaches a short-lived login-shell PATH probe from the
// caller's controlling-terminal session. Interactive login shells may enable job
// control and become the TTY foreground process group; running them in a new
// session prevents that from stopping the TUI with SIGTTIN.
func PrepareShellPATHProbe(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
