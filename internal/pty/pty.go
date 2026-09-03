package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/proc"
)

var (
	ErrSessionNotFound = errors.New("pty: session not found")
	ErrSessionClosed   = errors.New("pty: session is closed")
	ErrSessionExists   = errors.New("pty: session already exists")
)

// DefaultTerminalCols is the default width in characters.
const DefaultTerminalCols = 120

// DefaultTerminalRows is the default height in characters.
const DefaultTerminalRows = 40

// DefaultSilenceWindow is the period of inactivity after which output is considered settled.
const DefaultSilenceWindow = 200 * time.Millisecond

// DefaultReadMaxBytes is the maximum bytes returned in one read call if not specified (32 KiB).
const DefaultReadMaxBytes = 32 * 1024

// MaxReadBytes is the upper cap for a single read call (128 KiB).
const MaxReadBytes = 128 * 1024

// Size represents the terminal window dimensions.
type Size struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
	X    uint16 `json:"x,omitempty"`
	Y    uint16 `json:"y,omitempty"`
}

// SessionInfo is the read-only summary of an active or recent PTY session.
type SessionInfo struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	Cwd        string    `json:"cwd"`
	PID        int       `json:"pid"`
	Running    bool      `json:"running"`
	ExitCode   int       `json:"exit_code,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	LastActive time.Time `json:"last_active"`
	Cols       uint16    `json:"cols"`
	Rows       uint16    `json:"rows"`
}

// StartOptions configures the initialization of a new PTY session.
type StartOptions struct {
	ID      string            // Session identifier (defaults to "default" if empty)
	Command string            // Command/shell to run (defaults to default shell)
	Args    []string          // Command arguments
	Cwd     string            // Working directory
	Env     map[string]string // Additional environment variables
	Cols    uint16            // Terminal width (default 120)
	Rows    uint16            // Terminal height (default 40)
}

// LowLevelPTY represents the operating-system-specific pseudoterminal interface.
type LowLevelPTY interface {
	io.ReadWriteCloser
	Resize(rows, cols uint16) error
}

// Session is a single long-lived interactive PTY process.
type Session struct {
	id         string
	command    string
	cwd        string
	startedAt  time.Time
	lastActive time.Time
	cols       uint16
	rows       uint16

	cmd        *exec.Cmd
	lowPTY     LowLevelPTY
	buffer     *RingBuffer
	done       chan struct{}
	onOutput   chan struct{} // Notified whenever new bytes land in buffer
	exitCode   int
	exitErr    error
	running    atomic.Bool
	mu         sync.RWMutex
	closeOnce  sync.Once
}

// ID returns the unique session identifier.
func (s *Session) ID() string { return s.id }

// IsRunning reports whether the underlying process is alive.
func (s *Session) IsRunning() bool { return s.running.Load() }

// PID returns the OS process ID, or 0 if not running.
func (s *Session) PID() int {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Info returns a snapshot of the session state.
func (s *Session) Info() SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SessionInfo{
		ID:         s.id,
		Command:    s.command,
		Cwd:        s.cwd,
		PID:        s.PID(),
		Running:    s.running.Load(),
		ExitCode:   s.exitCode,
		StartedAt:  s.startedAt,
		LastActive: s.lastActive,
		Cols:       s.cols,
		Rows:       s.rows,
	}
}

// startOutputPump reads raw bytes from the low-level PTY into the ring buffer.
func (s *Session) startOutputPump() {
	buf := make([]byte, 4096)
	for {
		n, err := s.lowPTY.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.lastActive = time.Now()
			s.mu.Unlock()

			_, _ = s.buffer.Write(buf[:n])

			// Non-blocking notification to silence detectors
			select {
			case s.onOutput <- struct{}{}:
			default:
			}
		}
		if err != nil {
			break
		}
	}
}

// monitorProcess waits for the command to terminate and records exit code.
func (s *Session) monitorProcess() {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.running.Store(false)
	s.lastActive = time.Now()
	s.exitErr = err
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			s.exitCode = exitErr.ExitCode()
		} else {
			s.exitCode = -1
		}
	} else {
		s.exitCode = 0
	}
	s.mu.Unlock()

	close(s.done)
	_ = s.lowPTY.Close()
}

// Write sends input bytes to the PTY stdin and optionally waits for output to settle.
func (s *Session) Write(ctx context.Context, input string, waitBudget time.Duration) (string, error) {
	if !s.running.Load() {
		return "", ErrSessionClosed
	}

	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()

	// 1. Send input to the PTY device
	if len(input) > 0 {
		if _, err := s.lowPTY.Write([]byte(input)); err != nil {
			return "", fmt.Errorf("pty write failed: %w", err)
		}
	}

	// 2. If no wait requested, return immediately without reading
	if waitBudget <= 0 {
		return "", nil
	}

	// 3. Wait for output to settle using silence detection
	return s.waitForOutput(ctx, waitBudget)
}

// waitForOutput monitors the stream until output is quiet for DefaultSilenceWindow or timeout expires.
func (s *Session) waitForOutput(ctx context.Context, waitBudget time.Duration) (string, error) {
	deadline := time.Now().Add(waitBudget)
	silenceTimer := time.NewTimer(DefaultSilenceWindow)
	defer silenceTimer.Stop()

	hasReceivedOutput := false

	for {
		select {
		case <-ctx.Done():
			// Context cancelled; return whatever unread output we have accumulated so far
			return CleanBytes(s.buffer.ReadUnread(MaxReadBytes)), ctx.Err()

		case <-s.done:
			// Process terminated; drain remaining output
			time.Sleep(50 * time.Millisecond) // Allow pump to flush
			return CleanBytes(s.buffer.ReadUnread(MaxReadBytes)), nil

		case <-s.onOutput:
			// New bytes arrived, reset the silence window timer
			hasReceivedOutput = true
			if !silenceTimer.Stop() {
				select {
				case <-silenceTimer.C:
				default:
				}
			}
			silenceTimer.Reset(DefaultSilenceWindow)

		case <-silenceTimer.C:
			// Silence window expired — output stream has settled
			if hasReceivedOutput || time.Now().After(deadline) {
				return CleanBytes(s.buffer.ReadUnread(MaxReadBytes)), nil
			}
			// If we haven't received any output yet, keep waiting until overall deadline
			silenceTimer.Reset(DefaultSilenceWindow)
		}

		if time.Now().After(deadline) {
			return CleanBytes(s.buffer.ReadUnread(MaxReadBytes)), nil
		}
	}
}

// Read reads unread output from the buffer, or up to maxBytes.
func (s *Session) Read(maxBytes int) string {
	if maxBytes <= 0 || maxBytes > MaxReadBytes {
		maxBytes = DefaultReadMaxBytes
	}
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()

	raw := s.buffer.ReadUnread(maxBytes)
	return CleanBytes(raw)
}

// ReadTail returns the most recent output from the buffer without advancing unread position.
func (s *Session) ReadTail(maxBytes int) string {
	if maxBytes <= 0 || maxBytes > MaxReadBytes {
		maxBytes = DefaultReadMaxBytes
	}
	raw := s.buffer.ReadTail(maxBytes)
	return CleanBytes(raw)
}

// Resize changes the pseudo-terminal window dimensions.
func (s *Session) Resize(cols, rows uint16) error {
	if cols == 0 {
		cols = DefaultTerminalCols
	}
	if rows == 0 {
		rows = DefaultTerminalRows
	}
	s.mu.Lock()
	s.cols = cols
	s.rows = rows
	s.mu.Unlock()

	if s.lowPTY != nil {
		return s.lowPTY.Resize(rows, cols)
	}
	return nil
}

// Close gracefully terminates the PTY session and reaps its entire process tree.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.running.Store(false)
		if s.cmd != nil && s.cmd.Process != nil && s.cmd.Process.Pid > 0 {
			// 1. Stage 1: Send SIGINT to give child chance to save state
			signalSIGINT(s.cmd)
			select {
			case <-s.done:
			case <-time.After(200 * time.Millisecond):
				// 2. Stage 2: Send SIGTERM to process group
				signalSIGTERM(s.cmd)
				select {
				case <-s.done:
				case <-time.After(250 * time.Millisecond):
					// 3. Stage 3: Force kill entire process tree
					proc.KillTree(s.cmd)
				}
			}
		}
		if s.lowPTY != nil {
			_ = s.lowPTY.Close()
		}
	})
	return err
}
