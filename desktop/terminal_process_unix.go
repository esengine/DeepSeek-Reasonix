//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

type unixTerminalProcess struct {
	cmd       *exec.Cmd
	pty       *os.File
	closeOnce sync.Once
}

func terminalPlatformAvailable() (bool, string) {
	return true, ""
}

func startTerminalProcess(spec terminalStartSpec) (terminalProcess, error) {
	cmd := exec.Command(spec.command.path, spec.command.args...)
	cmd.Dir = spec.dir
	cmd.Env = spec.env
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(spec.rows), Cols: uint16(spec.cols)})
	if err != nil {
		return nil, err
	}
	return &unixTerminalProcess{cmd: cmd, pty: file}, nil
}

func (p *unixTerminalProcess) Read(data []byte) (int, error) {
	return p.pty.Read(data)
}

func (p *unixTerminalProcess) Write(data []byte) (int, error) {
	return p.pty.Write(data)
}

func (p *unixTerminalProcess) Resize(cols, rows int) error {
	return pty.Setsize(p.pty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (p *unixTerminalProcess) Wait() (int, error) {
	err := p.cmd.Wait()
	if p.cmd.ProcessState != nil {
		return p.cmd.ProcessState.ExitCode(), err
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), err
	}
	return -1, err
}

func (p *unixTerminalProcess) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		if p.cmd.Process != nil {
			// creack/pty starts the command in a new session. Killing the process
			// group prevents foreground children from surviving a closed tab.
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		}
		closeErr = p.pty.Close()
	})
	return closeErr
}
