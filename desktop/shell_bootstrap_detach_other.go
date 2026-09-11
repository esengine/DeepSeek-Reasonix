//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachShellProcess gives the shell its own session so the terminal that
// launched the bootstrap cannot hang it up after the bootstrap exits.
func detachShellProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
