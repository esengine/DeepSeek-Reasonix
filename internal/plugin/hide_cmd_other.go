//go:build !windows

package plugin

import "os/exec"

// hideCmdWindow is a no-op on non-Windows platforms.
func hideCmdWindow(cmd *exec.Cmd) {}
