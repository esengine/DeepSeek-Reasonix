package pty

import (
	"errors"
	"fmt"
	"sync"
)

// SessionState represents the current operational mode of a PTY process.
type SessionState string

const (
	// StateShellReady indicates the session is an interactive shell idle at a prompt,
	// ready to accept structured commands via write_line.
	StateShellReady SessionState = "shell_ready"

	// StateCommandRunning indicates the session is actively executing a command line.
	// In this state, write_line is forbidden; only read or raw write (e.g. Ctrl+C) are permitted.
	StateCommandRunning SessionState = "command_running"

	// StateRawInteractive indicates a non-shell process (e.g. Python REPL, Node, gdb).
	// In this state, commands cannot be run via write_line; input must go through write.
	StateRawInteractive SessionState = "raw_interactive"

	// StateClosed indicates the underlying process or PTY device has terminated.
	StateClosed SessionState = "closed"
)

var (
	ErrCommandRunningBusy = errors.New("pty: session is currently executing a command (state: command_running)")
	ErrNotShellSession    = errors.New("pty: session is in raw interactive mode (not a shell); use action=write / read")
)

// StateMachine manages thread-safe transitions for a Session.
type StateMachine struct {
	mu      sync.RWMutex
	current SessionState
	isShell bool
}

// NewStateMachine creates a state machine initialized to ShellReady or RawInteractive.
func NewStateMachine(isShell bool) *StateMachine {
	initial := StateShellReady
	if !isShell {
		initial = StateRawInteractive
	}
	return &StateMachine{
		current: initial,
		isShell: isShell,
	}
}

// Current returns the active state.
func (sm *StateMachine) Current() SessionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current
}

// CanWriteLine validates whether the session is eligible to start a write_line command.
func (sm *StateMachine) CanWriteLine() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	switch sm.current {
	case StateClosed:
		return ErrSessionClosed
	case StateCommandRunning:
		return fmt.Errorf("%w: wait for command to complete, read output with action=read, or interrupt it with action=write input=\"\\x03\"", ErrCommandRunningBusy)
	case StateRawInteractive:
		return fmt.Errorf("%w: write_line is only available for shell sessions; send input via action=write", ErrNotShellSession)
	case StateShellReady:
		return nil
	default:
		return fmt.Errorf("pty: unexpected session state %q", sm.current)
	}
}

// BeginCommand attempts to transition from ShellReady to CommandRunning atomically.
func (sm *StateMachine) BeginCommand() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.current == StateClosed {
		return ErrSessionClosed
	}
	if sm.current == StateCommandRunning {
		return ErrCommandRunningBusy
	}
	if sm.current != StateShellReady {
		return ErrNotShellSession
	}

	sm.current = StateCommandRunning
	return nil
}

// EndCommand transitions back from CommandRunning to ShellReady (if not closed).
func (sm *StateMachine) EndCommand() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.current == StateClosed {
		return
	}
	if sm.isShell {
		sm.current = StateShellReady
	} else {
		sm.current = StateRawInteractive
	}
}

// MarkClosed transitions to Closed.
func (sm *StateMachine) MarkClosed() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.current = StateClosed
}

// InterruptIfRunning transitions from CommandRunning to ShellReady if interrupted by Ctrl+C.
func (sm *StateMachine) InterruptIfRunning() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.current == StateCommandRunning {
		if sm.isShell {
			sm.current = StateShellReady
		} else {
			sm.current = StateRawInteractive
		}
		return true
	}
	return false
}
