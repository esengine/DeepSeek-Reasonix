package stats

import (
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
)

type turnCompletionSpy struct {
	spySink
	completions int
}

func (s *turnCompletionSpy) RecordTurnCompletion() { s.completions++ }

// A synchronous run reports its turn through RecordTurnCompletion; the
// recorder must pass it to the frontend sink it wraps, once, and must not
// turn a TurnDone event into a second completion.
func TestRecorderForwardsTurnCompletionOnce(t *testing.T) {
	inner := &turnCompletionSpy{}
	r := NewRecorder(inner, testenv.TempDir(t), "cli")
	event.RecordTurnCompletion(r)
	r.Emit(turnEvent())
	flushRecorder(t, r)
	if inner.completions != 1 {
		t.Fatalf("inner saw %d turn completions, want 1", inner.completions)
	}
	if len(inner.events) != 1 || inner.events[0].Kind != event.TurnDone {
		t.Fatalf("inner events = %+v, want the one TurnDone", inner.events)
	}
}
