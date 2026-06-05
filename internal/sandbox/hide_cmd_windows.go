//go:build windows

package sandbox

import (
	"os/exec"
	"syscall"
)

// hideCmdWindow prevents a subprocess from flashing a console window.
func hideCmdWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
