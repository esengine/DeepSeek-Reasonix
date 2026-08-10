package main

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

func correlatedSubmissionID(t *testing.T, payload any) (string, *int) {
	t.Helper()
	wire, ok := payload.(correlatedWireEventTab)
	if !ok {
		t.Fatalf("payload type = %T, want correlatedWireEventTab", payload)
	}
	return wire.SubmissionID, wire.CheckpointTurn
}

func TestTabEventSinkCorrelatesDelayedTurnDoneBySubmission(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan any, 2)
	sink := &tabEventSink{tabID: "tab", ctx: context.Background()}
	sink.runtimeEvents.emit = func(_ context.Context, _ string, payload ...any) {
		delivered <- payload[0]
		if len(delivered) == 1 {
			close(entered)
			<-release
		}
	}

	firstTurn := 0
	if !sink.tryBeginTurn("u-first") {
		t.Fatal("failed to reserve first turn")
	}
	sink.Emit(event.Event{Kind: event.TurnDone, CheckpointTurn: &firstTurn})
	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first runtime delivery did not start")
	}

	secondTurn := 1
	if !sink.tryBeginTurn("u-second") {
		t.Fatal("delayed frontend delivery blocked the next raw turn")
	}
	sink.Emit(event.Event{Kind: event.TurnDone, CheckpointTurn: &secondTurn})
	close(release)

	first := <-delivered
	second := <-delivered
	if id, turn := correlatedSubmissionID(t, first); id != "u-first" || turn == nil || *turn != 0 {
		t.Fatalf("first correlation = (%q, %v), want (u-first, 0)", id, turn)
	}
	if id, turn := correlatedSubmissionID(t, second); id != "u-second" || turn == nil || *turn != 1 {
		t.Fatalf("second correlation = (%q, %v), want (u-second, 1)", id, turn)
	}
}

func TestTabEventSinkClearsRejectedSubmissionCorrelation(t *testing.T) {
	delivered := make(chan any, 2)
	sink := &tabEventSink{tabID: "tab", ctx: context.Background()}
	sink.runtimeEvents.emit = func(_ context.Context, _ string, payload ...any) {
		delivered <- payload[0]
	}

	if !sink.tryBeginTurn("u-local-command") {
		t.Fatal("failed to reserve local command")
	}
	sink.Emit(event.Event{Kind: event.Notice, Text: "local result"})
	sink.cancelTurnStart()
	if id, gotTurn := correlatedSubmissionID(t, <-delivered); id != "u-local-command" || gotTurn != nil {
		t.Fatalf("local command correlation = (%q, %v), want (u-local-command, nil)", id, gotTurn)
	}
	sink.Emit(event.Event{Kind: event.Notice, Text: "late local notice"})
	if _, ok := (<-delivered).(wireEventTab); !ok {
		t.Fatal("event after rejected submission retained its correlation")
	}

	turn := 4
	if !sink.tryBeginTurn("u-model-turn") {
		t.Fatal("rejected local command left the sink reserved")
	}
	sink.Emit(event.Event{Kind: event.TurnDone, CheckpointTurn: &turn})
	if id, gotTurn := correlatedSubmissionID(t, <-delivered); id != "u-model-turn" || gotTurn == nil || *gotTurn != turn {
		t.Fatalf("model correlation = (%q, %v), want (u-model-turn, %d)", id, gotTurn, turn)
	}
}

func TestSubmitToTabWithIDCorrelatesOnlyAdmittedGuardedTurn(t *testing.T) {
	delivered := make(chan any, 8)
	sink := &tabEventSink{tabID: "tab", ctx: context.Background()}
	sink.runtimeEvents.emit = func(_ context.Context, _ string, payload ...any) {
		delivered <- payload[0]
	}
	ctrl := control.New(control.Options{Sink: sink})
	defer ctrl.Close()
	tab := &WorkspaceTab{ID: "tab", Scope: "global", Ready: true, Ctrl: ctrl, sink: sink}
	app := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}, activeTabID: tab.ID}

	if err := app.SubmitToTabWithID(tab.ID, "/tree", "u-local"); err != nil {
		t.Fatalf("local command: %v", err)
	}
	local := <-delivered
	if id, turn := correlatedSubmissionID(t, local); id != "u-local" || turn != nil {
		t.Fatalf("local correlation = (%q, %v), want (u-local, nil)", id, turn)
	}

	if err := app.SubmitToTabWithID(tab.ID, "/mcp__definitely_missing", "u-guarded"); err != nil {
		t.Fatalf("guarded command: %v", err)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case payload := <-delivered:
			wire, ok := payload.(correlatedWireEventTab)
			if !ok || wire.Kind != "turn_done" {
				continue
			}
			if wire.SubmissionID != "u-guarded" {
				t.Fatalf("guarded TurnDone submission = %q, want u-guarded", wire.SubmissionID)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for guarded TurnDone")
		}
	}
}

func TestTabEventSinkDropsCorrelationWhenFrontendBindingChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*tabEventSink)
	}{
		{name: "tab", change: func(s *tabEventSink) { s.setBinding("replacement", nil) }},
		{name: "runtime epoch", change: func(s *tabEventSink) { s.setRuntimeEpoch("runtime-2") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delivered := make(chan any, 1)
			sink := &tabEventSink{tabID: "original", ctx: context.Background(), runtimeEpoch: "runtime-1"}
			sink.runtimeEvents.emit = func(_ context.Context, _ string, payload ...any) {
				delivered <- payload[0]
			}
			if !sink.tryBeginTurn("u-original") {
				t.Fatal("failed to reserve original turn")
			}
			tc.change(sink)
			turn := 7
			sink.Emit(event.Event{Kind: event.TurnDone, CheckpointTurn: &turn})
			payload := <-delivered
			if _, ok := payload.(correlatedWireEventTab); ok {
				t.Fatal("changed frontend binding received the old submission correlation")
			}
			wire, ok := payload.(wireEventTab)
			if !ok || wire.CheckpointTurn == nil || *wire.CheckpointTurn != turn {
				t.Fatalf("uncorrelated payload = %#v, want checkpoint turn %d", payload, turn)
			}
		})
	}
}
