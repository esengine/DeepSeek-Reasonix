package pty

import (
	"testing"
)

func TestPTYSessionStateTransitions(t *testing.T) {
	// 1. Shell session starts in StateShellReady
	sm := NewStateMachine(true)
	if got := sm.Current(); got != StateShellReady {
		t.Fatalf("expected initial shell state to be ShellReady, got: %s", got)
	}
	if err := sm.CanWriteLine(); err != nil {
		t.Fatalf("CanWriteLine failed on ShellReady: %v", err)
	}

	// 2. BeginCommand moves to CommandRunning
	if err := sm.BeginCommand(); err != nil {
		t.Fatalf("BeginCommand failed: %v", err)
	}
	if got := sm.Current(); got != StateCommandRunning {
		t.Fatalf("expected state to be CommandRunning, got: %s", got)
	}

	// 3. Concurrent or repeated write_line is forbidden during CommandRunning
	if err := sm.CanWriteLine(); err == nil {
		t.Fatalf("expected CanWriteLine to be rejected during CommandRunning")
	}
	if err := sm.BeginCommand(); err == nil {
		t.Fatalf("expected BeginCommand to fail during CommandRunning")
	}

	// 4. EndCommand restores ShellReady
	sm.EndCommand()
	if got := sm.Current(); got != StateShellReady {
		t.Fatalf("expected EndCommand to restore ShellReady, got: %s", got)
	}
	if err := sm.CanWriteLine(); err != nil {
		t.Fatalf("expected CanWriteLine to succeed after EndCommand: %v", err)
	}

	// 5. Ctrl+C interruption restores ShellReady
	_ = sm.BeginCommand()
	if !sm.InterruptIfRunning() {
		t.Fatalf("expected InterruptIfRunning to return true when CommandRunning")
	}
	if got := sm.Current(); got != StateShellReady {
		t.Fatalf("expected Ctrl+C to restore ShellReady, got: %s", got)
	}

	// 6. MarkClosed transitions to Closed and rejects all commands
	sm.MarkClosed()
	if got := sm.Current(); got != StateClosed {
		t.Fatalf("expected state to be Closed, got: %s", got)
	}
	if err := sm.CanWriteLine(); err != ErrSessionClosed {
		t.Fatalf("expected ErrSessionClosed after MarkClosed, got: %v", err)
	}
}

func TestPTYRawInteractiveState(t *testing.T) {
	// Non-shell session (e.g. Python, gdb) starts in RawInteractive
	sm := NewStateMachine(false)
	if got := sm.Current(); got != StateRawInteractive {
		t.Fatalf("expected non-shell initial state to be RawInteractive, got: %s", got)
	}

	// write_line is strictly forbidden for raw interactive sessions
	if err := sm.CanWriteLine(); err == nil {
		t.Fatalf("expected CanWriteLine to be rejected for RawInteractive session")
	}
}
