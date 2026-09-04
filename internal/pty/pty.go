package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/proc"
)

// markerPrefix is embedded in the completion sentinel printed after every RunCommand.
const markerPrefix = "__REASONIX_DONE_"

// newRequestID generates a compact random hex tag for one command boundary.
func newRequestID() string {
	return fmt.Sprintf("%08x", rand.Uint32()) //nolint:gosec // non-crypto token
}

var (
	ErrSessionNotFound = errors.New("pty: session not found")
	ErrSessionClosed   = errors.New("pty: session is closed")
	ErrSessionExists   = errors.New("pty: session already exists")
	// ErrCommandRunning is returned by RunCommand when the wait budget expires
	// before the completion marker is received. The command is still running in
	// the PTY; the caller should poll with Read or wait with a longer budget.
	ErrCommandRunning = errors.New("pty: command still running (marker not received within wait budget)")
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

// CommandStatus indicates the execution disposition of a write_line command.
type CommandStatus string

const (
	StatusCompleted  CommandStatus = "completed"
	StatusRunning    CommandStatus = "running"
	StatusTerminated CommandStatus = "terminated"
)

// CommandResult represents the outcome of a structured shell command execution.
type CommandResult struct {
	Output   string        `json:"output"`
	ExitCode int           `json:"exit_code"`
	Status   CommandStatus `json:"status"`
}

// SessionInfo is the read-only summary of an active or recent PTY session.
type SessionInfo struct {
	ID         string       `json:"id"`
	Command    string       `json:"command"`
	Cwd        string       `json:"cwd"`
	PID        int          `json:"pid"`
	Running    bool         `json:"running"`
	State      SessionState `json:"state,omitempty"`
	ExitCode   int          `json:"exit_code,omitempty"`
	StartedAt  time.Time    `json:"started_at"`
	LastActive time.Time    `json:"last_active"`
	Cols       uint16       `json:"cols"`
	Rows       uint16       `json:"rows"`
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

	cmd       *exec.Cmd
	lowPTY    LowLevelPTY
	buffer    *RingBuffer
	done      chan struct{}
	onOutput  chan struct{} // Notified whenever new bytes land in buffer
	exitCode  int
	exitErr   error
	running   atomic.Bool
	mu        sync.RWMutex
	writeMu   sync.Mutex
	closeOnce sync.Once

	driver       ShellDriver
	state        *StateMachine
	parser       *CompletionParser
	activeMarker string

	// pendingInput accumulates raw interactive write() bytes that have not yet
	// been submitted (i.e. no \n seen).
	pendingInputMu sync.Mutex
	pendingInput   strings.Builder
}

// State returns the current session operational state.
func (s *Session) State() SessionState {
	if s.state == nil {
		if s.running.Load() {
			return StateShellReady
		}
		return StateClosed
	}
	return s.state.Current()
}

// PendingInput returns the current uncommitted input accumulated in the PTY line buffer.
func (s *Session) PendingInput() string {
	s.pendingInputMu.Lock()
	defer s.pendingInputMu.Unlock()
	return s.pendingInput.String()
}

// CommitRawInput is called AFTER a write is allowed and sent to the PTY.
// If raw contains control characters that abort the line (\x03 for Ctrl+C, \x15 for Ctrl+U),
// the pending line buffer is cleared.
// Otherwise, any text up to the last newline has been submitted to the shell, and
// any characters after the last newline become the new pending line buffer.
func (s *Session) CommitRawInput(raw string) {
	s.pendingInputMu.Lock()
	defer s.pendingInputMu.Unlock()
	if strings.ContainsAny(raw, "\x03\x15") {
		s.pendingInput.Reset()
		return
	}
	s.pendingInput.WriteString(raw)
	current := s.pendingInput.String()
	if idx := strings.LastIndex(current, "\n"); idx >= 0 {
		s.pendingInput.Reset()
		s.pendingInput.WriteString(current[idx+1:])
	}
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
		State:      s.State(),
		ExitCode:   s.exitCode,
		StartedAt:  s.startedAt,
		LastActive: s.lastActive,
		Cols:       s.cols,
		Rows:       s.rows,
	}
}

// startOutputPump reads raw bytes from the low-level PTY, feeds CompletionParser for state
// detection, and writes filtered user-visible output to the ring buffer.
func (s *Session) startOutputPump() {
	buf := make([]byte, 4096)
	for {
		n, err := s.lowPTY.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.lastActive = time.Now()
			s.mu.Unlock()

			chunk := buf[:n]
			userBytes := chunk
			if s.parser != nil {
				userBytes = s.parser.Feed(chunk)
			}
			if len(userBytes) > 0 {
				_, _ = s.buffer.Write(userBytes)
				// Non-blocking notification to silence detectors
				select {
				case s.onOutput <- struct{}{}:
				default:
				}
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

	if s.state != nil {
		s.state.MarkClosed()
	}
	close(s.done)
	_ = s.lowPTY.Close()
}

// RunCommand sends a complete shell command line and waits for its output using a
// deterministic request-id completion marker rather than a silence window.
func (s *Session) RunCommand(ctx context.Context, cmd string, waitBudget time.Duration) (CommandResult, error) {
	if !s.running.Load() {
		return CommandResult{Status: StatusTerminated}, ErrSessionClosed
	}

	if s.state != nil {
		if err := s.state.CanWriteLine(); err != nil {
			return CommandResult{Status: StatusRunning}, err
		}
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	requestID := newRequestID()
	marker := markerPrefix + requestID + "__"

	s.mu.Lock()
	s.lastActive = time.Now()
	s.activeMarker = marker
	s.mu.Unlock()

	if s.state != nil {
		if err := s.state.BeginCommand(requestID); err != nil {
			return CommandResult{Status: StatusRunning}, err
		}
	}

	driver := s.driver
	if driver == nil {
		driver = &PosixShellDriver{}
	}

	payload := driver.FormatCommand(cmd, marker)
	var notifyCh <-chan struct{}
	if s.parser != nil {
		notifyCh = s.parser.Arm(requestID, marker, driver)
	}

	if _, err := s.lowPTY.Write([]byte(payload)); err != nil {
		if s.state != nil {
			s.state.EndCommand()
		}
		if s.parser != nil {
			s.parser.Disarm()
		}
		return CommandResult{}, fmt.Errorf("pty write failed: %w", err)
	}
	s.CommitRawInput(payload)

	if waitBudget <= 0 {
		return CommandResult{Status: StatusRunning}, nil
	}

	return s.waitForCompletion(ctx, notifyCh, marker, waitBudget)
}

// waitForCompletion awaits the sentinel notification from CompletionParser or budget expiry.
func (s *Session) waitForCompletion(ctx context.Context, notifyCh <-chan struct{}, marker string, waitBudget time.Duration) (CommandResult, error) {
	timer := time.NewTimer(waitBudget)
	defer timer.Stop()

	select {
	case <-notifyCh:
		if s.state != nil {
			s.state.EndCommand()
		}
		out, exitCode, _ := s.parser.Result()
		return CommandResult{
			Output:   out,
			ExitCode: exitCode,
			Status:   StatusCompleted,
		}, nil

	case <-timer.C:
		// Budget expired: command is still running
		var out string
		if s.parser != nil {
			out, _, _ = s.parser.Result()
		}
		return CommandResult{
			Output: out,
			Status: StatusRunning,
		}, ErrCommandRunning

	case <-ctx.Done():
		if s.parser != nil {
			out, exitCode, matched := s.parser.Result()
			if matched {
				if s.state != nil {
					s.state.EndCommand()
				}
				return CommandResult{Output: out, ExitCode: exitCode, Status: StatusCompleted}, nil
			}
			return CommandResult{Output: out, Status: StatusRunning}, ctx.Err()
		}
		return CommandResult{Status: StatusRunning}, ctx.Err()

	case <-s.done:
		if s.state != nil {
			s.state.MarkClosed()
		}
		var out string
		var exitCode int
		var matched bool
		if s.parser != nil {
			out, exitCode, matched = s.parser.Result()
		}
		if matched {
			return CommandResult{Output: out, ExitCode: exitCode, Status: StatusCompleted}, nil
		}
		return CommandResult{
			Output:   out,
			ExitCode: s.exitCode,
			Status:   StatusTerminated,
		}, nil
	}
}

// Write sends input bytes to the PTY stdin and optionally waits for output to settle.
// Use this for raw interactive input (REPL prompts, gdb, Ctrl+C, etc.) where
// there is no "command completion" concept; silence-window settling is appropriate.
func (s *Session) Write(ctx context.Context, input string, waitBudget time.Duration) (string, error) {
	if !s.running.Load() {
		return "", ErrSessionClosed
	}

	if s.state != nil && s.state.Current() == StateClosed {
		return "", ErrSessionClosed
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()

	// 1. Send input to the PTY device; commit to pending buffer only upon successful write
	if len(input) > 0 {
		payload := []byte(input)
		if strings.Contains(input, "\x03") && s.state != nil && s.state.Current() == StateCommandRunning {
			s.mu.RLock()
			marker := s.activeMarker
			s.mu.RUnlock()
			if marker != "" {
				probe := fmt.Sprintf("\nprintf '\\n%s:130\\n'\n", marker)
				payload = append(payload, []byte(probe)...)
			}
		}

		if _, err := s.lowPTY.Write(payload); err != nil {
			return "", fmt.Errorf("pty write failed: %w", err)
		}
		s.CommitRawInput(input)
	}

	// 2. If no wait requested, return immediately without reading
	if waitBudget <= 0 {
		return "", nil
	}

	// 3. Wait for output to settle using silence detection
	return s.waitForOutput(ctx, input, waitBudget)
}

// waitForOutput monitors the stream until output is quiet for DefaultSilenceWindow or timeout expires.
func (s *Session) waitForOutput(ctx context.Context, input string, waitBudget time.Duration) (string, error) {
	deadline := time.Now().Add(waitBudget)
	silenceTimer := time.NewTimer(DefaultSilenceWindow)
	defer silenceTimer.Stop()

	hasReceivedOutput := false
	inputClean := strings.TrimSpace(CleanBytes([]byte(input)))
	isSubmittingCmd := strings.Contains(input, "\n")

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
			// If submitting a command line, do not treat the terminal's local echo of the input as output completion
			if isSubmittingCmd && time.Now().Before(deadline) {
				peeked := strings.TrimSpace(CleanBytes(s.buffer.PeekUnread(MaxReadBytes)))
				if peeked == inputClean || peeked == "" {
					silenceTimer.Reset(DefaultSilenceWindow)
					continue
				}
			}

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
			signalSIGINT(s.cmd)
			select {
			case <-s.done:
			case <-time.After(200 * time.Millisecond):
				signalSIGTERM(s.cmd)
				select {
				case <-s.done:
				case <-time.After(250 * time.Millisecond):
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
