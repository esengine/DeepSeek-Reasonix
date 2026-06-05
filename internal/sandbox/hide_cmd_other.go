//go:build !windows

package sandbox

import "os/exec"

// hideCmdWindow is a no-op on non-Windows platforms.
func hideCmdWindow(cmd *exec.Cmd) {}
