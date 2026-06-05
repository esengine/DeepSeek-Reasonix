//go:build !windows

package lsp

import "os/exec"

// hideCmdWindow is a no-op on non-Windows platforms.
func hideCmdWindow(cmd *exec.Cmd) {}
