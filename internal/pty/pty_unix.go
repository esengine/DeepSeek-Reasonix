//go:build !windows

package pty

import (
	"fmt"
	"os"
	"os/exec"

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

	ptmx, err := cpty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, fmt.Errorf("failed to start unix pty: %w", err)
	}

	return &unixLowLevelPTY{ptmx: ptmx}, nil
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
