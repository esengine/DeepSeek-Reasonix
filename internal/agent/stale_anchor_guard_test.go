package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestDeleteRangeRequiresReadAfterSameTurnWrite(t *testing.T) {
	var deleteCalls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "delete_range", readOnly: false, calls: &deleteCalls})

	args := `{"path":"src/map.html","start_anchor":"before","end_anchor":"after"}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "delete_range", args),
			toolCallChunk("c2", "delete_range", args),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "edit the map"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&deleteCalls); got != 1 {
		t.Fatalf("delete_range executed %d times, want only the first call", got)
	}
	results := toolResults(a.session, "delete_range")
	if len(results) != 2 {
		t.Fatalf("tool results = %d, want 2", len(results))
	}
	last := results[len(results)-1]
	for _, want := range []string{"[fresh read required]", "read_file", "multi_edit"} {
		if !strings.Contains(last, want) {
			t.Fatalf("blocked result should mention %q, got %q", want, last)
		}
	}
}

func TestEditFileAllowedAfterSameTurnWriteWithoutFreshRead(t *testing.T) {
	var editCalls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "edit_file", readOnly: false, calls: &editCalls})

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "edit_file", `{"path":"src/map.html","old_string":"before","new_string":"after"}`),
			toolCallChunk("c2", "edit_file", `{"path":"src/map.html","old_string":"other","new_string":"updated"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "edit two independent regions"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&editCalls); got != 2 {
		t.Fatalf("edit_file executed %d times, want both optimistic exact-match edits", got)
	}
	if last := lastToolResult(a.session, "edit_file"); strings.Contains(last, "[fresh read required]") {
		t.Fatalf("edit_file should rely on its current-file uniqueness check, got %q", last)
	}
}

func TestDeleteRangeAllowedAfterFreshRead(t *testing.T) {
	var deleteCalls int32
	var readCalls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "delete_range", readOnly: false, calls: &deleteCalls})
	reg.Add(fakeTool{name: "read_file", readOnly: true, calls: &readCalls})

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "delete_range", `{"path":"src/map.html","start_anchor":"before","end_anchor":"after"}`),
			toolCallChunk("c2", "read_file", `{"path":"src/map.html"}`),
			toolCallChunk("c3", "delete_range", `{"path":"src/map.html","start_anchor":"current","end_anchor":"final"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "edit the map with a read between edits"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&readCalls); got != 1 {
		t.Fatalf("read_file executed %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&deleteCalls); got != 2 {
		t.Fatalf("delete_range executed %d times, want 2 after fresh read", got)
	}
	if last := lastToolResult(a.session, "delete_range"); strings.Contains(last, "[fresh read required]") {
		t.Fatalf("fresh read should allow the second edit, got %q", last)
	}
}

func TestDeleteRangeStillRequiresReadAfterWindowedRead(t *testing.T) {
	var deleteCalls int32
	var readCalls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "delete_range", readOnly: false, calls: &deleteCalls})
	reg.Add(fakeTool{name: "read_file", readOnly: true, calls: &readCalls})

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "delete_range", `{"path":"src/map.html","start_anchor":"before","end_anchor":"after"}`),
			toolCallChunk("c2", "read_file", `{"path":"src/map.html","offset":400,"limit":20}`),
			toolCallChunk("c3", "delete_range", `{"path":"src/map.html","start_anchor":"current","end_anchor":"final"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "edit the map with a narrow read between edits"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&readCalls); got != 1 {
		t.Fatalf("read_file executed %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&deleteCalls); got != 1 {
		t.Fatalf("delete_range executed %d times, want only the first call", got)
	}
	if last := lastToolResult(a.session, "delete_range"); !strings.Contains(last, "[fresh read required]") {
		t.Fatalf("windowed read should not allow the second edit, got %q", last)
	}
}

func TestMultiEditAllowedAfterSameTurnWrite(t *testing.T) {
	var editCalls int32
	var multiCalls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "edit_file", readOnly: false, calls: &editCalls})
	reg.Add(fakeTool{name: "multi_edit", readOnly: false, calls: &multiCalls})

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "edit_file", `{"path":"src/map.html","old_string":"before","new_string":"after"}`),
			toolCallChunk("c2", "multi_edit", `{"path":"src/map.html","edits":[{"old_string":"current","new_string":"final"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "edit the map atomically"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&editCalls); got != 1 {
		t.Fatalf("edit_file executed %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&multiCalls); got != 1 {
		t.Fatalf("multi_edit executed %d times, want 1", got)
	}
}
