// Copyright (c) 2025, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"os/exec"
	"syscall"
)

func killCommandOnTestExit(cmd *exec.Cmd) {
	// It's incredibly easy to let an external shell loop forever by accident.
	// In those cases, kill it as soon as the Go test process finishes.
	//
	// Similarly, when killing the shell process due to a timeout,
	// also kill any children processes that the shell spawned via a group.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
		Setpgid:   true,
	}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
