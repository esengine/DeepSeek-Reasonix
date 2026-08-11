package acp

import (
	"testing"

	"reasonix/internal/event"
)

// TestUpdateSinkSkipsPartialDispatch probes the ACP adapter against the early
// (Partial) ToolDispatch. The adapter's contract is one pending tool_call per
// call, carrying rawInput; the partial has no args, so without a skip the client
// gets two tool_call notifications, the first with empty input.
func TestUpdateSinkSkipsPartialDispatch(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")

	sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call-1", Name: "read_file", Partial: true}})
	sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call-1", Name: "read_file", Args: `{"path":"a.go"}`}})
	sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call-1", Name: "read_file", Args: `{"path":"a.go"}`, Refreshed: true}})

	if got := len(fn.notifs); got != 1 {
		t.Fatalf("want one tool_call notification, got %d", got)
	}
}
