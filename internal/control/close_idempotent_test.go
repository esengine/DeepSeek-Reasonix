package control

import (
	"context"
	"sync/atomic"
	"testing"

	"reasonix/internal/hook"
)

// TestCloseIsIdempotent guards the desktop tab-lifecycle contract: rebind,
// model switch, CloseTab, and shutdown can race to Close the same controller,
// so a duplicate Close must not re-fire SessionEnd hooks or re-run cleanup.
func TestCloseIsIdempotent(t *testing.T) {
	var sessionEnds atomic.Int32
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "session-end"},
		Event:      hook.SessionEnd,
		Scope:      hook.ScopeGlobal,
	}}, t.TempDir(), func(context.Context, hook.SpawnInput) hook.SpawnResult {
		sessionEnds.Add(1)
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
	c := New(Options{Runner: &fakeTurnRunner{}, Hooks: hooks})
	// A completed turn arms startedOnce so SessionEnd is eligible to fire.
	if err := c.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		c.Close()
		close(done)
	}()
	c.Close()
	<-done

	if got := sessionEnds.Load(); got != 1 {
		t.Fatalf("SessionEnd hooks fired %d times across concurrent Close calls, want 1", got)
	}
}
