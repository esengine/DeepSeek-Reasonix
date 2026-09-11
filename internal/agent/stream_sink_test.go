package agent

import (
	"reflect"
	"testing"

	"reasonix/internal/event"
)

func sinkKinds(evs []event.Event) []event.Kind {
	kinds := make([]event.Kind, 0, len(evs))
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

func TestDeferredStreamSinkBuffersSpeculativeEventsUntilReasoning(t *testing.T) {
	inner := &recordSink{}
	s := newReasoningAwareStreamSink(inner)

	s.Emit(event.Event{Kind: event.Text, Text: "a"})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1"}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c1"}})
	s.Emit(event.Event{Kind: event.Message, Text: "m"})
	// Usage/turn bookkeeping is never speculative and passes through live, as
	// does an empty reasoning metadata event (it must not unlock the buffer).
	s.Emit(event.Event{Kind: event.Usage})
	s.Emit(event.Event{Kind: event.Reasoning, Text: "  "})
	s.Emit(event.Event{Kind: event.Text, Text: "b"})

	if got, want := sinkKinds(inner.evs), []event.Kind{event.Usage, event.Reasoning}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pre-reasoning events = %v, want only %v", got, want)
	}

	s.Emit(event.Event{Kind: event.Reasoning, Text: "thinking"})
	want := []event.Kind{
		event.Usage, event.Reasoning, event.Reasoning,
		event.Text, event.ToolDispatch, event.ToolResult, event.Message, event.Text,
	}
	if got := sinkKinds(inner.evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("unlock flush = %v, want reasoning then buffered order %v", got, want)
	}

	s.Emit(event.Event{Kind: event.Text, Text: "c"})
	if got := sinkKinds(inner.evs); !reflect.DeepEqual(got, append(want, event.Text)) {
		t.Fatalf("post-unlock events = %v, want live passthrough", got)
	}
}

func TestDeferredStreamSinkDiscardDropsBufferedEvents(t *testing.T) {
	inner := &recordSink{}
	s := newReasoningAwareStreamSink(inner)
	s.Emit(event.Event{Kind: event.Text, Text: "speculative"})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1"}})
	s.Discard()
	s.Flush()

	if got := len(inner.evs); got != 0 {
		t.Fatalf("discarded sink emitted %d buffered events, want 0", got)
	}
	s.Emit(event.Event{Kind: event.Reasoning, Text: "thinking"})
	s.Emit(event.Event{Kind: event.Text, Text: "adopted"})
	if got, want := sinkKinds(inner.evs), []event.Kind{event.Reasoning, event.Text}; !reflect.DeepEqual(got, want) {
		t.Fatalf("post-discard events = %v, want %v", got, want)
	}
}

func TestDeferredStreamSinkDefersEverythingUntilFlush(t *testing.T) {
	inner := &recordSink{}
	s := newDeferredStreamSink(inner)
	s.Emit(event.Event{Kind: event.Reasoning, Text: "thinking"})
	s.Emit(event.Event{Kind: event.Usage})
	s.Emit(event.Event{Kind: event.Text, Text: "a"})
	if got := len(inner.evs); got != 0 {
		t.Fatalf("defer-all sink leaked %d events before Flush", got)
	}
	s.Flush()
	want := []event.Kind{event.Reasoning, event.Usage, event.Text}
	if got := sinkKinds(inner.evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("flushed events = %v, want arrival order %v", got, want)
	}
	s.Flush()
	if got := len(inner.evs); got != len(want) {
		t.Fatalf("second Flush re-emitted events: %d total", got)
	}
}

func TestDeferredStreamSinkNilIsInert(t *testing.T) {
	var s *deferredStreamSink
	s.Emit(event.Event{Kind: event.Text, Text: "x"})
	s.Flush()
	s.Discard()
}
