//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"golang.org/x/sys/windows"

	"reasonix/internal/proc"
)

// remoteSSHProcess owns the Windows Job Object for one OpenSSH tree. The
// mutex closes the cmd.Start/context-cancel adoption race: a cancellation that
// arrives before StartTracked returns is remembered and applied to the handle
// as soon as it becomes available.
type remoteSSHProcess struct {
	mu            sync.Mutex
	job           uintptr
	killRequested bool
	finished      bool
}

func newRemoteSSHProcess(cmd *exec.Cmd) *remoteSSHProcess {
	process := &remoteSSHProcess{}
	// Reasonix Desktop is a GUI process. OpenSSH must never allocate or flash a
	// console window; StartTrackedRequired preserves this flag when it adds
	// CREATE_SUSPENDED for fail-closed Job Object adoption.
	proc.HideWindow(cmd)
	cmd.WaitDelay = remoteSSHWaitDelay
	cmd.Cancel = func() error {
		if process.kill(cmd) {
			return nil
		}
		return os.ErrProcessDone
	}
	return process
}

func (p *remoteSSHProcess) start(cmd *exec.Cmd) error {
	job, err := proc.StartTrackedRequired(cmd)
	if err != nil {
		if errors.Is(err, proc.ErrProcessTrackingUnavailable) {
			return errRemoteSSHProcessIsolation
		}
		return err
	}
	p.mu.Lock()
	killRequested := p.killRequested
	finished := p.finished
	if !killRequested && !finished {
		p.job = job
	}
	p.mu.Unlock()
	if killRequested {
		proc.KillTracked(cmd, job)
	} else if finished {
		proc.FinishTracked(job)
	}
	return nil
}

// wait observes root-process exit through its still-owned Windows handle,
// marks this lifecycle finished, and only then lets exec.Cmd reap the process.
// That ordering is important: once cmd.Wait releases the process handle, the
// PID may be reused, so Close must already be unable to fall through to
// taskkill /PID by that point.
func (p *remoteSSHProcess) wait(cmd *exec.Cmd) error {
	return p.waitWithReaper(cmd, cmd.Wait)
}

// waitWithReaper keeps the final exec.Cmd reap injectable so the exact
// exit-observed/finished/reap boundary can be tested deterministically.
func (p *remoteSSHProcess) waitWithReaper(cmd *exec.Cmd, reap func() error) error {
	exitWaitErr := waitForRemoteSSHProcessExit(cmd)
	if exitWaitErr != nil {
		// A handle-wait failure leaves liveness unknown. Terminate while the
		// os.Process handle still pins this PID, then finish before reaping.
		p.kill(cmd)
	}
	p.finish()
	reapErr := reap()
	if reapErr != nil {
		return reapErr
	}
	return exitWaitErr
}

func waitForRemoteSSHProcessExit(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("OpenSSH process is unavailable")
	}
	var waitErr error
	if err := cmd.Process.WithHandle(func(rawHandle uintptr) {
		result, err := windows.WaitForSingleObject(windows.Handle(rawHandle), windows.INFINITE)
		if err != nil {
			waitErr = fmt.Errorf("wait for OpenSSH process handle: %w", err)
			return
		}
		if result != windows.WAIT_OBJECT_0 {
			waitErr = fmt.Errorf("wait for OpenSSH process handle returned %d", result)
		}
	}); err != nil {
		return fmt.Errorf("borrow OpenSSH process handle: %w", err)
	}
	return waitErr
}

// kill returns true only for the caller that takes ownership of tree
// termination. Context cancellation and live Close therefore share one
// idempotent path.
func (p *remoteSSHProcess) kill(cmd *exec.Cmd) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	if p.killRequested || p.finished {
		p.mu.Unlock()
		return false
	}
	p.killRequested = true
	job := p.job
	p.job = 0
	p.mu.Unlock()
	proc.KillTracked(cmd, job)
	return true
}

func (p *remoteSSHProcess) finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.finished {
		p.mu.Unlock()
		return
	}
	p.finished = true
	job := p.job
	p.job = 0
	p.mu.Unlock()
	proc.FinishTracked(job)
}
