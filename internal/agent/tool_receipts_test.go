package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
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
	if results := sink.kinds(event.ToolResult); len(results) != 1 || results[0].Tool.Name != "todo_write" {
		t.Fatalf("completed todo result must be checkpointed before the next tool: %+v", results)
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

func TestTodoEventsUseCanonicalHostArgsWithoutRewritingHistory(t *testing.T) {
	raw := `{"todos":[{"content":"A","status":"in_progress"},{"content":"B","status":"completed"}]}`
	initial := []evidence.TodoItem{
		{Content: "A", Status: "in_progress"},
		{Content: "B", Status: "pending"},
	}
	session := NewSession("")
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "todo-1", Name: "todo_write", Arguments: raw,
	}}})
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "todo_write", Success: true, Todos: initial})
	sink := &recordSink{}
	todoTool := fakeTool{name: "todo_write", readOnly: true}
	a := &Agent{
		svc:  agentServices{sink: sink, tools: tool.NewRegistry()},
		sess: sessionRuntime{conversation: session, todoState: append([]evidence.TodoItem(nil), initial...)},
		task: taskRuntime{ledger: ledger},
	}
	a.svc.tools.Add(todoTool)
	call := provider.ToolCall{ID: "todo-1", Name: "todo_write", Arguments: raw}
	plan := &toolCallPlan{
		call:         call,
		tool:         todoTool,
		readOnly:     true,
		evidenceName: "todo_write",
		evidenceArgs: json.RawMessage(raw),
		cctx:         evidence.WithLedger(context.Background(), ledger),
	}
	output := a.recordToolReceipts(plan, "Todos updated", nil, nil)
	if output == "" || plan.hostTodoState == nil || len(plan.hostTodoState.Deferred) != 1 || plan.hostTodoState.Deferred[0].ID == "" {
		t.Fatalf("todo receipt did not produce canonical host state: output=%q state=%+v", output, plan.hostTodoState)
	}
	if err := a.emitBatchToolResult(call, toolOutcome{runState: provider.ToolRunCompleted, output: output, hostTodoState: plan.hostTodoState}, 0, 0, false, time.Time{}); err != nil {
		t.Fatalf("emitBatchToolResult: %v", err)
	}
	wantArgs := `{"todos":[{"content":"A","status":"in_progress"},{"content":"B","status":"pending"}]}`
	previews := sink.kinds(event.ToolResultPreview)
	if len(previews) != 1 || previews[0].Tool.Args != wantArgs {
		t.Fatalf("preview args = %+v, want canonical %s", previews, wantArgs)
	}
	results := sink.kinds(event.ToolResult)
	if len(results) != 1 || results[0].Tool.Args != wantArgs {
		t.Fatalf("result args = %+v, want canonical %s", results, wantArgs)
	}
	stored := session.Snapshot()[0].ToolCalls[0].Arguments
	if stored != raw {
		t.Fatalf("historical ToolCall arguments changed to %q, want %q", stored, raw)
	}
}
