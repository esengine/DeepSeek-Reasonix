package agent

import (
	"testing"
	"time"

	"reasonix/internal/event"
)

func TestSubagentProgressPartialIsTerminal(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 16)
	trk := newTestTracker(t, clock, chanSink{ch: ch}, "child-partial")
	trk.running()
	_ = waitEvent(t, ch, "running status")
	trk.finish(nil, &SubagentRunError{Outcome: SubagentOutcome{Status: SubagentOutcomePartial, Retryable: true}})
	statuses := collectFor(t, ch, 100*time.Millisecond)
	if len(statuses) != 1 || statuses[0].Tool.Output != string(subagentPhasePartial) {
		t.Fatalf("partial terminal events = %+v, want one partial status", statuses)
	}
}
