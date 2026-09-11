//go:build !windows

package proc

import "os/exec"

// HideWindow is a no-op off Windows.
func HideWindow(*exec.Cmd) {}

// HideConsole is a no-op off Windows.
func HideConsole(*exec.Cmd) {}
