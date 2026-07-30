//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

type remoteSSHProcess struct {
	killOnce sync.Once
	killErr  error
}

func newRemoteSSHProcess(cmd *exec.Cmd) *remoteSSHProcess {
	process := &remoteSSHProcess{}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if process.kill(cmd) {
			return process.killErr
		}
		return os.ErrProcessDone
	}
	cmd.WaitDelay = remoteSSHWaitDelay
	return process
}

func (*remoteSSHProcess) start(cmd *exec.Cmd) error { return cmd.Start() }

func (*remoteSSHProcess) wait(cmd *exec.Cmd) error { return cmd.Wait() }

func (p *remoteSSHProcess) kill(cmd *exec.Cmd) bool {
	if p == nil {
		return false
	}
	killed := false
	p.killOnce.Do(func() {
		killed = true
		if cmd == nil || cmd.Process == nil {
			return
		}
		p.killErr = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(p.killErr, syscall.ESRCH) {
			p.killErr = nil
		}
	})
	return killed
}
