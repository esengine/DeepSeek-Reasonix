//go:build windows

package proc

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// HideWindow stops a child process from flashing a console window on Windows,
// where a GUI parent (the desktop app) has no console of its own to inherit.
// CREATE_NO_WINDOW suppresses the console a console child (git, rg, a shell)
// would otherwise pop; HideWindow guards any GUI child that shows a window.
func HideWindow(cmd *exec.Cmd) {
	HideConsole(cmd)
	cmd.SysProcAttr.HideWindow = true
}

// HideConsole prevents a console window without hiding a GUI application's
// first window. Use it for launchers that can start either console or GUI code.
func HideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
