//go:build windows

package proc

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow  = 0x08000000 // CREATE_NO_WINDOW
	detachedProcess = 0x00000008 // DETACHED_PROCESS
)

// HideWindow stops a child process from flashing a console window on Windows,
// where a GUI parent (the desktop app) has no console of its own to inherit.
// CREATE_NO_WINDOW suppresses the console a console child (git, rg, a shell)
// would otherwise pop; HideWindow guards any GUI child that shows a window.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// HideWindowDetached is for short-lived desktop-only probes whose descendants
// have been observed to flash their own console windows. Do not use it for tools
// that rely on console-program stdout/stderr capture, such as PowerShell.
func HideWindowDetached(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= detachedProcess
}
