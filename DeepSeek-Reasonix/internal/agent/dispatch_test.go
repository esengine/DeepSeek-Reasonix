package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestEarlyToolDispatch proves a ChunkToolCallStart surfaces a ToolDispatch
// immediately (Partial, name only) so the card shows while the arguments are
// still streaming, and that a second, full dispatch (with args) follows once the
// call completes — the fix for "the edit_file card only appears after everything
// is written".
func TestEarlyToolDispatch(t *testing.T) {
	prov := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "c1", Name: "read_file"}},
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"/x"}`}},
		{Type: provider.ChunkDone},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})

	var got []event.Event
	sink := event.FuncSink(func(e event.Event) { got = append(got, e) })
	a := New(prov, reg, NewSession(""), Options{MaxSteps: 1}, sink)
	_ = a.Run(context.Background(), "go") // errors at the 1-step cap; we only want the events

	var partial, full int
	for _, e := range got {
		if e.Kind != event.ToolDispatch {
			continue
		}
		if e.Tool.Partial {
			partial++
			if e.Tool.Name != "read_file" {
				t.Errorf("partial dispatch name = %q, want read_file", e.Tool.Name)
			}
		} else {
			full++
			if e.Tool.Args == "" {
				t.Errorf("full dispatch should carry args, got none")
			}
		}
	}
	if partial != 1 {
		t.Errorf("want 1 partial (early) dispatch, got %d", partial)
	}
	if full != 1 {
		t.Errorf("want 1 full dispatch, got %d", full)
	}
}
