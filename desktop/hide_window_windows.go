//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideAndStart starts the given command with its console window hidden.
func hideAndStart(name string) error {
	cmd := exec.Command(name)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return hideAndStartCmd(cmd)
}

// hideAndStartCmd starts the given command with its console window hidden.
func hideAndStartCmd(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
	return cmd.Start()
}
