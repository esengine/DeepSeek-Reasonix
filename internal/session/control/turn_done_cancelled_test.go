package control

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/contract/event"
)

// The Cancelled flag is the identity frontends trust over Err, so a stop that
// raced a real failure must not hide the failure behind it.
func TestTurnDoneIsCancelledOnlyWhenTheTurnEndedOnTheSentinel(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", context.Canceled, true},
		{"wrapped sentinel", errors.Join(errors.New("tool"), context.Canceled), true},
		{"no error", nil, true},
		{"real failure", errors.New("provider down"), false},
		{"deadline", context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		var got event.Event
		c := New(Options{Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				got = e
			}
		})})
		c.mu.Lock()
		c.gate.running, c.gate.canceling = true, true
		c.mu.Unlock()
		c.finishGuardedTurn(tc.err, &guardedTurnCompletion{})
		if got.Cancelled != tc.want {
			t.Errorf("%s: Cancelled = %v, want %v", tc.name, got.Cancelled, tc.want)
		}
	}
}
