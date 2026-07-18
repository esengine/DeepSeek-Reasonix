package host

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

func TestRuntimeSnapshotRetainsSemanticItemsBeyondDeprecatedRawLimit(t *testing.T) {
	const notices = 5000
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 4)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := runtime.Submit(context.Background(), "many visible notices")
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	for index := 0; index < notices; index++ {
		controller.emit(event.Event{Kind: event.Notice, Text: fmt.Sprintf("notice-%04d", index)})
	}
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BoundarySeq != notices+1 {
		t.Fatalf("boundary = %d, want %d", snapshot.BoundarySeq, notices+1)
	}
	if len(snapshot.Events) != notices+1 {
		t.Fatalf("semantic events = %d, want TurnStarted + %d notices", len(snapshot.Events), notices)
	}
	if snapshot.Events[0].Event.Kind != "turn_started" || snapshot.Events[1].Event.Text != "notice-0000" || snapshot.Events[len(snapshot.Events)-1].Event.Text != "notice-4999" {
		t.Fatalf("semantic order first=%+v second=%+v last=%+v", snapshot.Events[0].Event, snapshot.Events[1].Event, snapshot.Events[len(snapshot.Events)-1].Event)
	}
	for index, envelope := range snapshot.Events {
		if envelope.Seq != 0 {
			t.Fatalf("snapshot semantic event[%d] retained realtime seq %d", index, envelope.Seq)
		}
		if envelope.TurnID != submitted.TurnID || envelope.HostEpoch != snapshot.HostEpoch || envelope.RuntimeEpoch != snapshot.RuntimeEpoch || envelope.Target != snapshot.Target {
			t.Fatalf("snapshot semantic envelope[%d] identity = %+v", index, envelope)
		}
	}

	// Snapshot events are defensive copies of actor-owned semantic state.
	snapshot.Events[1].Event.Text = "caller mutation"
	again, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.Events[1].Event.Text != "notice-0000" {
		t.Fatalf("caller mutation escaped snapshot: %+v", again.Events[1].Event)
	}
}

func TestRuntimeSnapshotCoalescesTwelveThousandStreamingDeltas(t *testing.T) {
	const deltas = 12001
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 2)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Submit(context.Background(), "large streaming answer"); err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	var wantText, wantReasoning strings.Builder
	for index := 0; index < deltas; index++ {
		chunk := fmt.Sprintf("%d;", index)
		if index%2 == 0 {
			wantReasoning.WriteString(chunk)
			controller.emit(event.Event{Kind: event.Reasoning, Text: chunk})
		} else {
			wantText.WriteString(chunk)
			controller.emit(event.Event{Kind: event.Text, Text: chunk})
		}
	}
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BoundarySeq != deltas+1 {
		t.Fatalf("boundary = %d, want %d", snapshot.BoundarySeq, deltas+1)
	}
	if len(snapshot.Events) != 3 {
		t.Fatalf("%d deltas reduced to %d semantic events, want TurnStarted + text + reasoning", deltas, len(snapshot.Events))
	}
	var gotText, gotReasoning string
	for _, envelope := range snapshot.Events {
		switch envelope.Event.Kind {
		case "text":
			gotText = envelope.Event.Text
		case "reasoning":
			gotReasoning = envelope.Event.Text
		}
	}
	if gotText != wantText.String() || gotReasoning != wantReasoning.String() {
		t.Fatalf("coalesced stream bytes text=%d/%d reasoning=%d/%d", len(gotText), wantText.Len(), len(gotReasoning), wantReasoning.Len())
	}

	// Mutating one returned projection cannot affect the reducer or a later
	// protocol-facing projection.
	for index := range snapshot.Events {
		if snapshot.Events[index].Event.Kind == "reasoning" {
			snapshot.Events[index].Event.Text = "mutated"
		}
	}
	again, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if eventByKind(again.Events, "reasoning").Text != wantReasoning.String() {
		t.Fatal("stream projection was not a deep copy")
	}
}

func TestRuntimeAppliesSemanticEventBeforeRealtimeNotification(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 8, 1)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Submit(context.Background(), "ordered event"); err != nil {
		t.Fatal(err)
	}
	subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	factory.controller(0).emit(event.Event{Kind: event.Notice, Text: "visible-before-notify"})
	message := receiveMessage(t, subscription.Messages)
	if message.Event == nil || message.Event.Seq != 2 || message.Event.Event.Text != "visible-before-notify" {
		t.Fatalf("realtime notification = %+v", message)
	}
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BoundarySeq != message.Event.Seq || eventByKind(snapshot.Events, "notice").Text != "visible-before-notify" {
		t.Fatalf("notification outran semantic snapshot: boundary=%d events=%+v", snapshot.BoundarySeq, snapshot.Events)
	}
}

func eventByKind(events []RuntimeEvent, kind string) eventwire.Event {
	for _, envelope := range events {
		if envelope.Event.Kind == kind {
			return envelope.Event
		}
	}
	return eventwire.Event{}
}

func countLivePromptEvents(snapshot RuntimeSnapshot) int {
	count := 0
	for _, envelope := range snapshot.Events {
		if envelope.Event.Kind == "approval_request" || envelope.Event.Kind == "ask_request" {
			count++
		}
	}
	return count
}
