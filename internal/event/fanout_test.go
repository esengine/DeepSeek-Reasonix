package event

import (
	"reflect"
	"testing"
)

// recordingSink captures every event it receives, in order, for assertions.
type recordingSink struct {
	events []Event
}

func (r *recordingSink) Emit(e Event) {
	r.events = append(r.events, e)
}

func TestFanOutLen(t *testing.T) {
	if got := NewFanOut().Len(); got != 0 {
		t.Errorf("Len() with no sinks = %d, want 0", got)
	}
	if got := NewFanOut(&recordingSink{}, &recordingSink{}).Len(); got != 2 {
		t.Errorf("Len() with two sinks = %d, want 2", got)
	}
	// Nil sinks still count as registered.
	if got := NewFanOut(nil, nil).Len(); got != 2 {
		t.Errorf("Len() with two nil sinks = %d, want 2", got)
	}
}

func TestFanOutZeroSinksIsNoop(t *testing.T) {
	// Emitting to a fan-out with no registered sinks must not panic.
	NewFanOut().Emit(Event{Kind: Text, Text: "hello"})
}

func TestFanOutSkipsNilSinks(t *testing.T) {
	rec := &recordingSink{}
	f := NewFanOut(nil, rec, nil)
	f.Emit(Event{Kind: Text, Text: "hello"})
	if len(rec.events) != 1 {
		t.Fatalf("sink received %d events, want 1 (nil sinks skipped)", len(rec.events))
	}
	if got := rec.events[0]; got.Kind != Text || got.Text != "hello" {
		t.Errorf("event = %+v, want Text/hello", got)
	}
}

func TestFanOutDeliversToEverySinkInOrder(t *testing.T) {
	first, second := &recordingSink{}, &recordingSink{}
	f := NewFanOut(first, second)

	events := []Event{
		{Kind: TurnStarted},
		{Kind: Text, Text: "first"},
		{Kind: ToolDispatch, Tool: Tool{Name: "bash"}},
		{Kind: ToolResult, Tool: Tool{Output: "ok"}},
		{Kind: TurnDone},
	}
	for _, e := range events {
		f.Emit(e)
	}

	if !reflect.DeepEqual(first.events, events) {
		t.Errorf("first sink got %+v, want %+v", first.events, events)
	}
	if !reflect.DeepEqual(second.events, events) {
		t.Errorf("second sink got %+v, want %+v", second.events, events)
	}
}
