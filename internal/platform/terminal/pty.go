package terminal

import "io"

// pty is the platform's pseudo-terminal around one running shell.
type pty interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	// Wait returns the shell's exit code once it ends.
	Wait() (int, error)
	Kill() error
	// Busy is whether the shell is running something it started and waits on.
	Busy() bool
}
