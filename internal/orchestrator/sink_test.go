package orchestrator

import (
	"sync"
	"testing"

	"reasonix/internal/event"
)

func collectSink() (event.Sink, func() []event.Event) {
	var mu sync.Mutex
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	return sink, func() []event.Event {
		mu.Lock()
		defer mu.Unlock()
		out := make([]event.Event, len(events))
		copy(out, events)
		return out
	}
}

func TestNewSinkMultiplexer(t *testing.T) {
	parent, _ := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")

	if m.agentName != "agent-x" {
		t.Fatalf("expected agentName 'agent-x', got %q", m.agentName)
	}
	if m.parentSink == nil {
		t.Fatal("parentSink should not be nil")
	}
	if m.verbose {
		t.Fatal("default verbose should be false")
	}
}

func TestSinkSetParentID(t *testing.T) {
	parent, _ := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")

	if id := m.ParentID(); id != "" {
		t.Fatalf("expected empty parent ID, got %q", id)
	}

	m.SetParentID("call-42")
	if id := m.ParentID(); id != "call-42" {
		t.Fatalf("expected parent ID 'call-42', got %q", id)
	}
}

func TestSinkSetVerbose(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")

	m.SetVerbose(true)
	m.Emit(event.Event{Kind: event.Message, Text: "hello"})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event with verbose=true, got %d", len(events))
	}
	if events[0].Kind != event.Message {
		t.Fatalf("expected Message kind, got %v", events[0].Kind)
	}
	if events[0].Text != "[agent-x] hello" {
		t.Fatalf("expected text '[agent-x] hello', got %q", events[0].Text)
	}
}

func TestSinkEmitToolDispatch(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")

	m.Emit(event.Event{
		Kind: event.ToolDispatch,
		Tool: event.Tool{ID: "tool-1", Name: "ls"},
	})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Kind != event.ToolDispatch {
		t.Fatalf("expected ToolDispatch kind")
	}
	if e.Tool.ID != "tool-1" {
		t.Fatalf("expected tool ID 'tool-1', got %q", e.Tool.ID)
	}
	if e.Tool.ParentID != "" {
		t.Fatalf("expected empty ParentID, got %q", e.Tool.ParentID)
	}
}

func TestSinkEmitToolDispatchWithParentID(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")
	m.SetParentID("call-42")

	m.Emit(event.Event{
		Kind: event.ToolDispatch,
		Tool: event.Tool{ID: "tool-1", Name: "ls"},
	})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Tool.ParentID != "call-42" {
		t.Fatalf("expected ParentID 'call-42', got %q", e.Tool.ParentID)
	}
	if e.Tool.ID != "call-42/tool-1" {
		t.Fatalf("expected tool ID 'call-42/tool-1', got %q", e.Tool.ID)
	}
}

func TestSinkEmitToolResultWithParentID(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")
	m.SetParentID("call-42")

	m.Emit(event.Event{
		Kind: event.ToolResult,
		Tool: event.Tool{ID: "tool-1", Output: "done"},
	})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Tool.ParentID != "call-42" {
		t.Fatalf("expected ParentID 'call-42', got %q", e.Tool.ParentID)
	}
	if e.Tool.ID != "tool-1" {
		t.Fatalf("ToolResult should NOT prefix ID with parent; got %q", e.Tool.ID)
	}
}

func TestSinkEmitToolProgressWithParentID(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")
	m.SetParentID("call-42")

	m.Emit(event.Event{
		Kind: event.ToolProgress,
		Tool: event.Tool{ID: "tool-1", Output: "progressing..."},
	})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Tool.ParentID != "call-42" {
		t.Fatalf("expected ParentID 'call-42', got %q", e.Tool.ParentID)
	}
}

func TestSinkEmitUsage(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")

	m.Emit(event.Event{Kind: event.Usage})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != event.Usage {
		t.Fatalf("expected Usage kind")
	}
}

func TestSinkEmitMessageVerbose(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")
	m.SetVerbose(true)

	m.Emit(event.Event{Kind: event.Message, Text: "hello world"})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Kind != event.Message {
		t.Fatalf("expected Message kind")
	}
	want := "[agent-x] hello world"
	if e.Text != want {
		t.Fatalf("expected text %q, got %q", want, e.Text)
	}
}

func TestSinkEmitMessageNonVerbose(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")
	m.SetVerbose(false)

	m.Emit(event.Event{Kind: event.Message, Text: "should be dropped"})

	events := get()
	if len(events) != 0 {
		t.Fatalf("expected 0 events with verbose=false, got %d", len(events))
	}
}

func TestSinkEmitReasoningVerbose(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")
	m.SetVerbose(true)

	m.Emit(event.Event{Kind: event.Reasoning, Text: "thinking..."})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != event.Reasoning {
		t.Fatalf("expected Reasoning kind")
	}
}

func TestSinkEmitReasoningNonVerbose(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")
	m.SetVerbose(false)

	m.Emit(event.Event{Kind: event.Reasoning, Text: "should be dropped"})

	events := get()
	if len(events) != 0 {
		t.Fatalf("expected 0 events with verbose=false, got %d", len(events))
	}
}

func TestSinkEmitNotice(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")

	m.Emit(event.Event{Kind: event.Notice, Text: "something happened"})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	want := "[agent-x] something happened"
	if e.Text != want {
		t.Fatalf("expected text %q, got %q", want, e.Text)
	}
}

func TestSinkEmitPhase(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")

	m.Emit(event.Event{Kind: event.Phase, Text: "planning"})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	want := "agent-x: planning"
	if e.Text != want {
		t.Fatalf("expected text %q, got %q", want, e.Text)
	}
}

func TestSinkEmitUnknownKind(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")

	m.Emit(event.Event{Kind: event.Kind(999), Text: "unknown"})

	events := get()
	if len(events) != 0 {
		t.Fatalf("expected 0 events for unknown kind, got %d", len(events))
	}
}

func TestSinkParentIDClearedOnNewAgent(t *testing.T) {
	parent, _ := collectSink()
	m1 := NewSinkMultiplexer(parent, "agent-a")
	m2 := NewSinkMultiplexer(parent, "agent-b")

	m1.SetParentID("call-1")
	m2.SetParentID("call-2")

	if id := m1.ParentID(); id != "call-1" {
		t.Fatalf("m1 expected 'call-1', got %q", id)
	}
	if id := m2.ParentID(); id != "call-2" {
		t.Fatalf("m2 expected 'call-2', got %q", id)
	}
}

func TestSinkToolDispatchNestedParentID(t *testing.T) {
	parent, get := collectSink()
	m := NewSinkMultiplexer(parent, "agent-x")
	m.SetParentID("call-1")

	m.Emit(event.Event{
		Kind: event.ToolDispatch,
		Tool: event.Tool{ID: "nested-tool"},
	})

	events := get()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	// Should be nested: call-1/nested-tool
	if events[0].Tool.ID != "call-1/nested-tool" {
		t.Fatalf("expected ID 'call-1/nested-tool', got %q", events[0].Tool.ID)
	}
}
