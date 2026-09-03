//go:build windows

package pty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

type windowsLowLevelPTY struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (w *windowsLowLevelPTY) Read(p []byte) (int, error) {
	return w.stdout.Read(p)
}

func (w *windowsLowLevelPTY) Write(p []byte) (int, error) {
	return w.stdin.Write(p)
}

func (w *windowsLowLevelPTY) Close() error {
	_ = w.stdin.Close()
	return w.stdout.Close()
}

func (w *windowsLowLevelPTY) Resize(rows, cols uint16) error {
	// Standard Windows pipe fallback does not support TIOCSWINSZ
	return nil
}

// spawnOSPTY starts a command with standard input/output redirection on Windows.
func spawnOSPTY(cmd *exec.Cmd, cols, rows uint16) (LowLevelPTY, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("windows pty stdin pipe failed: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("windows pty stdout pipe failed: %w", err)
	}
	cmd.Stderr = cmd.Stdout // Merge stderr into stdout stream

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("failed to start windows command: %w", err)
	}

	return &windowsLowLevelPTY{
		stdin:  stdin,
		stdout: stdout,
	}, nil
}

// defaultShellPath returns PowerShell or cmd.exe on Windows.
func defaultShellPath() string {
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "powershell.exe"
}
