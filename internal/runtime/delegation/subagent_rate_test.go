package delegation

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

// forty is 40 estimated tokens of Latin text (four bytes to a token).
var forty = strings.Repeat("a", 160)

func TestSubagentStatusCarriesAnEstimatedRateWhileItStreams(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	trk := newTestTracker(t, clock, chanSink{ch: ch}, "child-1")
	trk.running()
	waitEvent(t, ch, "running status")

	trk.wrap().Emit(event.Event{Kind: event.Reasoning, Text: forty})
	clock.Advance(subagentProgressMergeWindow)
	waitEvent(t, ch, "reasoning status")
	clock.Advance(time.Second - subagentProgressMergeWindow)
	trk.wrap().Emit(event.Event{Kind: event.Reasoning, Text: forty})

	var rate event.Event
	for {
		e := waitEvent(t, ch, "status with a rate")
		if e.Tool.Name == event.SubagentProgressStatusName && e.Tool.RateTokensPerSec > 0 {
			rate = e
			break
		}
	}
	if rate.Tool.Output != string(subagentPhaseReasoning) || rate.Tool.RateTokensPerSec != 40 {
		t.Fatalf("status = %q at %d tokens/s, want reasoning at 40", rate.Tool.Output, rate.Tool.RateTokensPerSec)
	}
}

func TestSubagentRateIsNotReportedInAToolCall(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	ch := make(chan event.Event, 64)
	trk := newTestTracker(t, clock, chanSink{ch: ch}, "child-1")
	trk.running()
	waitEvent(t, ch, "running status")
	trk.wrap().Emit(event.Event{Kind: event.Reasoning, Text: forty})
	clock.Advance(time.Second)
	trk.wrap().Emit(event.Event{Kind: event.Reasoning, Text: forty})
	clock.Advance(time.Second)
	trk.wrap().Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "t1", Name: "bash"}})
	clock.Advance(subagentProgressMergeWindow)

	for {
		e := waitEvent(t, ch, "tool phase status")
		if e.Tool.Name != event.SubagentProgressStatusName || e.Tool.Output != string(subagentPhaseTool) {
			continue
		}
		if e.Tool.RateTokensPerSec != 0 {
			t.Fatalf("tool status rate = %d, want zero", e.Tool.RateTokensPerSec)
		}
		break
	}
}

func TestSubagentRateWindowForgetsWhatFellOutOfIt(t *testing.T) {
	var m rateMeter
	t0 := time.Unix(0, 0)
	m.add(t0, 40)
	m.add(t0.Add(time.Second), 40)
	m.add(t0.Add(2*time.Second), 40)
	if r, ok := m.rate(t0.Add(2 * time.Second)); !ok || r != 40 {
		t.Fatalf("steady stream = %d, %v, want 40", r, ok)
	}
	// Ten quiet seconds, then a slow trickle: the old burst no longer counts.
	m.add(t0.Add(12*time.Second), 4)
	m.add(t0.Add(14*time.Second), 4)
	if r, ok := m.rate(t0.Add(14 * time.Second)); !ok || r != 2 {
		t.Fatalf("after the lull = %d, %v, want 2", r, ok)
	}
}

func TestSubagentRateNeedsASecondOfStream(t *testing.T) {
	var m rateMeter
	t0 := time.Unix(0, 0)
	m.add(t0, 40)
	m.add(t0.Add(300*time.Millisecond), 40)
	if _, ok := m.rate(t0.Add(300 * time.Millisecond)); ok {
		t.Fatal("a rate was reported after 300ms of stream")
	}
}
