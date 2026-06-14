package agent

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

func TestFinalReadinessFallsBackToCanonicalTodos(t *testing.T) {
	ran := evidence.Receipt{ToolName: "bash", Success: true, Command: "git push"}
	open := []evidence.TodoItem{{Content: "push", Status: "completed"}, {Content: "rebase", Status: "pending"}}

	// Turn did work (a successful bash) but issued no todo_write this turn, so the
	// per-turn ledger has no list — the canonical state must still gate.
	a := &Agent{evidence: readinessLedger(ran), todoState: open}
	if got := a.finalReadinessFailure(); !strings.Contains(got, "incomplete") {
		t.Fatalf("cross-turn gate = %q, want it to report incomplete canonical todos", got)
	}

	// A turn that touched nothing (pure Q&A) must never gate on stale canonical state.
	idle := &Agent{evidence: evidence.NewLedger(), todoState: open}
	if got := idle.finalReadinessFailure(); got != "" {
		t.Fatalf("no-work turn gated on canonical todos: %q", got)
	}

	// All canonical items completed → no gate even after work.
	done := &Agent{evidence: readinessLedger(ran), todoState: []evidence.TodoItem{{Content: "push", Status: "completed"}}}
	if got := done.finalReadinessFailure(); got != "" {
		t.Fatalf("completed canonical todos still gated: %q", got)
	}
}

func TestAdvanceCanonicalTodoCompletesAndPromotes(t *testing.T) {
	a := &Agent{
		sink: event.Discard,
		todoState: []evidence.TodoItem{
			{Content: "sync branch", Status: "in_progress"},
			{Content: "push to origin", Status: "pending"},
			{Content: "rebase", Status: "pending"},
		},
	}
	a.advanceCanonicalTodo("sync branch")

	if a.todoState[0].Status != "completed" {
		t.Fatalf("signed-off item not completed: %+v", a.todoState[0])
	}
	if a.todoState[1].Status != "in_progress" {
		t.Fatalf("next pending item not promoted: %+v", a.todoState[1])
	}
	if a.todoState[2].Status != "pending" {
		t.Fatalf("a later item was promoted out of order: %+v", a.todoState[2])
	}
}

func TestAdvanceCanonicalTodoMatchesByNumber(t *testing.T) {
	a := &Agent{sink: event.Discard, todoState: []evidence.TodoItem{
		{Content: "first", Status: "in_progress"},
		{Content: "second", Status: "pending"},
	}}
	a.advanceCanonicalTodo("2")
	if a.todoState[1].Status != "completed" {
		t.Fatalf("numeric step did not complete the second todo: %+v", a.todoState)
	}
}

func TestRebuildTodoStateReplaysCompleteSteps(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "t1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"a","status":"in_progress"},{"content":"b","status":"pending"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "todo_write", Content: "Todos updated"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "complete_step", Arguments: `{"step":"a"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "complete_step", Content: "signed off"},
	}
	a := &Agent{}
	a.rebuildTodoState(msgs)

	if len(a.todoState) != 2 {
		t.Fatalf("rebuilt %d todos, want 2", len(a.todoState))
	}
	if a.todoState[0].Status != "completed" {
		t.Fatalf("complete_step not replayed onto canonical state: %+v", a.todoState[0])
	}
	if a.todoState[1].Status != "in_progress" {
		t.Fatalf("next item not promoted on replay: %+v", a.todoState[1])
	}
}

func TestRebuildTodoStateSkipsFailedCompleteStep(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "t1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"a","status":"in_progress"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "todo_write", Content: "Todos updated"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "complete_step", Arguments: `{"step":"a"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "complete_step", Content: "error: no evidence"},
	}
	a := &Agent{}
	a.rebuildTodoState(msgs)

	if a.todoState[0].Status == "completed" {
		t.Fatalf("a failed complete_step must not advance canonical state: %+v", a.todoState[0])
	}
}
