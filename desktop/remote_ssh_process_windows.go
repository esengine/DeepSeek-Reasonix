//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

func configureRemoteSSHProcess(cmd *exec.Cmd) {
	cmd.WaitDelay = remoteSSHWaitDelay
}

func terminateRemoteSSHProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
