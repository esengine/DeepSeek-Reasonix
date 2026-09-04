//go:build windows

package pty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"reasonix/internal/proc"
)

// windowsPipeLowLevelPTY provides a reliable standard pipe-based interactive terminal
// fallback for Windows environments.
type windowsPipeLowLevelPTY struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
	closed bool
}

func (w *windowsPipeLowLevelPTY) Read(p []byte) (int, error) {
	return w.stdout.Read(p)
}

func (w *windowsPipeLowLevelPTY) Write(p []byte) (int, error) {
	return w.stdin.Write(p)
}

func (w *windowsPipeLowLevelPTY) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	_ = w.stdin.Close()
	return w.stdout.Close()
}

func (w *windowsPipeLowLevelPTY) Resize(rows, cols uint16) error {
	// Standard Windows pipe fallback does not support VT window resize
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

	return &windowsPipeLowLevelPTY{
		stdin:  stdin,
		stdout: stdout,
	}, nil
}

// signalSIGINT sends interrupt on Windows.
func signalSIGINT(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
}

// signalSIGTERM terminates the process tree on Windows.
func signalSIGTERM(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	proc.KillTree(cmd)
}

// defaultShellPath returns PowerShell or cmd.exe on Windows.
func defaultShellPath() string {
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "powershell.exe"
}
