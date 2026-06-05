//go:build !windows

package main

import "os/exec"

// hideAndStart starts the given command normally.
func hideAndStart(name string) error {
	cmd := exec.Command(name)
	return hideAndStartCmd(cmd)
}

// hideAndStartCmd starts the given command normally.
func hideAndStartCmd(cmd *exec.Cmd) error {
	return cmd.Start()
}
