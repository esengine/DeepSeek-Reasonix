package agent

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

// TestTextSinkSkipsPartialDispatch probes the headless sink against the early
// (Partial) ToolDispatch the agent emits when a call begins streaming. The chat
// TUI skips it; if TextSink prints it too, every tool call shows a duplicate
// "-> name" line (the first with empty args) in piped / `reasonix run` output.
func TestTextSinkSkipsPartialDispatch(t *testing.T) {
	var b strings.Builder
	s := NewTextSink(&b, nil, 80)

	s.Emit(event.Event{Kind: event.TurnStarted})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1", Name: "read_file", Partial: true}})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1", Name: "read_file", Args: `{"path":"a"}`}})

	got := b.String()
	if n := strings.Count(got, "-> read_file"); n != 1 {
		t.Errorf("want exactly one dispatch line, got %d:\n%q", n, got)
	}
}
