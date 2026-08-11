//go:build windows

package main

import (
	"context"
	"errors"
	"sync"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

type windowsTerminalProcess struct {
	pty       *conpty.ConPty
	closeOnce sync.Once
}

func terminalPlatformAvailable() (bool, string) {
	if !conpty.IsConPtyAvailable() {
		return false, "integrated terminal requires Windows 10 version 1809 or newer"
	}
	return true, ""
}

func startTerminalProcess(spec terminalStartSpec) (terminalProcess, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, conpty.ErrConPtyUnsupported
	}
	commandLine := windows.ComposeCommandLine(append([]string{spec.command.path}, spec.command.args...))
	p, err := conpty.Start(
		commandLine,
		conpty.ConPtyDimensions(spec.cols, spec.rows),
		conpty.ConPtyWorkDir(spec.dir),
		conpty.ConPtyEnv(spec.env),
	)
	if err != nil {
		return nil, err
	}
	return &windowsTerminalProcess{pty: p}, nil
}

func (p *windowsTerminalProcess) Read(data []byte) (int, error) {
	return p.pty.Read(data)
}

func (p *windowsTerminalProcess) Write(data []byte) (int, error) {
	return p.pty.Write(data)
}

func (p *windowsTerminalProcess) Resize(cols, rows int) error {
	return p.pty.Resize(cols, rows)
}

func (p *windowsTerminalProcess) Wait() (int, error) {
	code, err := p.pty.Wait(context.Background())
	if err != nil && errors.Is(err, context.Canceled) {
		return -1, err
	}
	return int(code), err
}

func (p *windowsTerminalProcess) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		// Closing the pseudo console terminates the attached process tree and
		// releases all ConPTY handles.
		closeErr = p.pty.Close()
	})
	return closeErr
}
