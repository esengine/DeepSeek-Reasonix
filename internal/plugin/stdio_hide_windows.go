//go:build windows

package plugin

import (
	"os/exec"
	"syscall"
)

// setPlatformAttrs applies Windows-specific process creation attributes to an
// exec.Cmd. On Windows, this hides the child process's window so that a
// console-mode subprocess (e.g. CodeGraph MCP) doesn't create a visible CMD
// window when the parent is a GUI app (built with -H windowsgui).
func setPlatformAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
