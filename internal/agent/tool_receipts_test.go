package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type toolReceiptSignalSink struct {
	mu       sync.Mutex
	events   []event.Event
	previews chan event.Event
}

func (s *toolReceiptSignalSink) Emit(e event.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	if e.Kind == event.ToolResultPreview {
		s.previews <- e
	}
}

func (s *toolReceiptSignalSink) kinds(kind event.Kind) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestTodoResultPreviewPreservesSingleProviderOrderedTerminalResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "todo_write", readOnly: true})
	reg.Add(blockingTool{name: "slow_read", started: started, release: release})
	sink := &toolReceiptSignalSink{previews: make(chan event.Event, 1)}
	a := New(nil, reg, NewSession(""), Options{}, sink)
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{
			{ID: "todo-1", Name: "todo_write", Arguments: `{"todos":[{"content":"Ship the fix","status":"in_progress"}]}`},
			{ID: "read-1", Name: "slow_read", Arguments: `{}`},
		})
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("later tool did not start")
	}
	select {
	case preview := <-sink.previews:
		if preview.Tool.ID != "todo-1" || preview.Tool.Name != "todo_write" || preview.Tool.Err != "" {
			t.Fatalf("todo preview = %+v", preview.Tool)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("todo result preview did not arrive while the later tool was running")
	}
	if results := sink.kinds(event.ToolResult); len(results) != 0 {
		t.Fatalf("terminal ToolResult published before the batch completed: %+v", results)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("batch did not finish after releasing the later tool")
	}
	if previews := sink.kinds(event.ToolResultPreview); len(previews) != 1 {
		t.Fatalf("ToolResultPreview events = %d, want 1", len(previews))
	}
	results := sink.kinds(event.ToolResult)
	if len(results) != 2 || results[0].Tool.ID != "todo-1" || results[1].Tool.ID != "read-1" {
		t.Fatalf("provider-ordered ToolResult events = %+v", results)
	}
}
