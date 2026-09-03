//go:build !windows

package pty

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	cpty "github.com/creack/pty"
)

type unixLowLevelPTY struct {
	ptmx *os.File
}

func (u *unixLowLevelPTY) Read(p []byte) (int, error) {
	return u.ptmx.Read(p)
}

func (u *unixLowLevelPTY) Write(p []byte) (int, error) {
	return u.ptmx.Write(p)
}

func (u *unixLowLevelPTY) Close() error {
	return u.ptmx.Close()
}

func (u *unixLowLevelPTY) Resize(rows, cols uint16) error {
	return cpty.Setsize(u.ptmx, &cpty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// spawnOSPTY starts a command attached to a real Unix pseudo-terminal device.
// It places the child in its own process group / session for clean tree-level termination.
func spawnOSPTY(cmd *exec.Cmd, cols, rows uint16) (LowLevelPTY, error) {
	if cols == 0 {
		cols = DefaultTerminalCols
	}
	if rows == 0 {
		rows = DefaultTerminalRows
	}

	ws := &cpty.Winsize{
		Rows: rows,
		Cols: cols,
	}

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid:  true,
			Setctty: true,
		}
	}

	ptmx, err := cpty.StartWithAttrs(cmd, ws, cmd.SysProcAttr)
	if err != nil {
		// Fallback to basic StartWithSize if custom attrs fail
		ptmx, err = cpty.StartWithSize(cmd, ws)
		if err != nil {
			return nil, fmt.Errorf("failed to start unix pty: %w", err)
		}
	}

	return &unixLowLevelPTY{ptmx: ptmx}, nil
}

// signalSIGINT sends SIGINT to the process group.
func signalSIGINT(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
}

// signalSIGTERM sends SIGTERM to the process group.
func signalSIGTERM(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
	}
}

// defaultShellPath returns the standard user login shell on Unix.
func defaultShellPath() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	if _, err := os.Stat("/bin/zsh"); err == nil {
		return "/bin/zsh"
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}
