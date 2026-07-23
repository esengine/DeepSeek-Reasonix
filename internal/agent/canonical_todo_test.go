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

func TestAdvanceCanonicalTodoRejectsPendingMatchByNumber(t *testing.T) {
	a := &Agent{sink: event.Discard, todoState: []evidence.TodoItem{
		{Content: "first", Status: "in_progress"},
		{Content: "second", Status: "pending"},
	}}
	a.advanceCanonicalTodo("2")
	if a.todoState[1].Status != "pending" || a.todoState[0].Status != "in_progress" {
		t.Fatalf("pending numeric step advanced out of order: %+v", a.todoState)
	}
}

func TestRebuildTodoStateIgnoresHistoricalPendingSignoff(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "t1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"a","status":"in_progress"},{"content":"b","status":"pending"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "todo_write", Content: "Todos updated"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "complete_step", Arguments: `{"step":"b"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "complete_step", Content: "signed off"},
	}
	a := &Agent{}
	a.rebuildTodoState(msgs)
	if a.todoState[0].Status != "in_progress" || a.todoState[1].Status != "pending" {
		t.Fatalf("historical pending signoff restored invalid state: %+v", a.todoState)
	}
}

func TestSetTodoStateNormalizesLegacyOutOfOrderSnapshot(t *testing.T) {
	a := &Agent{}
	a.setTodoState([]evidence.TodoItem{
		{Content: "first", Status: "in_progress"},
		{Content: "second", Status: "completed"},
	})
	if a.todoState[0].Status != "in_progress" || a.todoState[1].Status != "pending" {
		t.Fatalf("legacy snapshot was not normalized: %+v", a.todoState)
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

func TestRebuildTodoStateRequiresToolResults(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "t1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"a","status":"in_progress"}]}`,
		}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "complete_step", Arguments: `{"step":"a"}`,
		}}},
	}
	a := &Agent{}
	a.rebuildTodoState(msgs)
	if len(a.todoState) != 0 {
		t.Fatalf("todo_write without tool result rebuilt canonical state: %+v", a.todoState)
	}

	msgs = append(msgs[:1],
		provider.Message{Role: provider.RoleTool, ToolCallID: "t1", Name: "todo_write", Content: "Todos updated"},
		msgs[1],
	)
	a.rebuildTodoState(msgs)
	if len(a.todoState) != 1 {
		t.Fatalf("successful todo_write did not rebuild canonical state: %+v", a.todoState)
	}
	if got := a.todoState[0].Status; got != "in_progress" {
		t.Fatalf("complete_step without tool result changed status to %q", got)
	}
}

func TestRebuildTodoStateHonorsEmptyTodoWriteClear(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID:        "t1",
			Name:      "todo_write",
			Arguments: `{"todos":[{"content":"a","status":"in_progress"}]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "todo_write", Content: "Todos updated"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID:        "t2",
			Name:      "todo_write",
			Arguments: `{"todos":[]}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "t2", Name: "todo_write", Content: "Todos updated"},
	}
	a := &Agent{}
	a.rebuildTodoState(msgs)
	if len(a.todoState) != 0 {
		t.Fatalf("empty todo_write should clear rebuilt canonical state: %+v", a.todoState)
	}
}

func TestSeedTodoState(t *testing.T) {
	a := &Agent{sink: event.Discard}
	todos := []evidence.TodoItem{
		{Content: "step 1", Status: "in_progress"},
		{Content: "step 2", Status: "pending"},
	}
	a.SeedTodoState(todos)
	if len(a.todoState) != 2 {
		t.Fatalf("SeedTodoState: got %d items, want 2", len(a.todoState))
	}
	if a.todoState[0].Status != "in_progress" {
		t.Fatalf("SeedTodoState: first item status = %q, want in_progress", a.todoState[0].Status)
	}
}

func TestSeedTodoStateReplacesExisting(t *testing.T) {
	a := &Agent{sink: event.Discard, todoState: []evidence.TodoItem{
		{Content: "existing", Status: "in_progress"},
	}}
	a.SeedTodoState([]evidence.TodoItem{
		{Content: "new", Status: "in_progress"},
	})
	if len(a.todoState) != 1 || a.todoState[0].Content != "new" {
		t.Fatalf("SeedTodoState did not replace existing state: %+v", a.todoState)
	}
}

func TestSeedTodoStateAllowsAdvanceAfterSeed(t *testing.T) {
	a := &Agent{sink: event.Discard}
	a.SeedTodoState([]evidence.TodoItem{
		{Content: "step 1", Status: "in_progress"},
		{Content: "step 2", Status: "pending"},
	})
	a.advanceCanonicalTodo("step 1")
	if a.todoState[0].Status != "completed" {
		t.Fatalf("advance after seed: item 0 status = %q, want completed", a.todoState[0].Status)
	}
	if a.todoState[1].Status != "in_progress" {
		t.Fatalf("advance after seed: item 1 status = %q, want in_progress", a.todoState[1].Status)
	}
}

func TestAdvanceCanonicalTodoWalksPhaseChain(t *testing.T) {
	a := &Agent{sink: event.Discard, todoState: []evidence.TodoItem{
		{Content: "Port the parser", Status: "pending"},
		{Content: "move files", Status: "in_progress", Level: 1},
		{Content: "fix imports", Status: "pending", Level: 1},
		{Content: "Ship it", Status: "pending"},
		{Content: "run tests", Status: "pending", Level: 1},
	}}

	a.advanceCanonicalTodo("Port the parser")
	if a.todoState[0].Status != "pending" {
		t.Fatalf("pending phase advanced ahead of its sub-steps: %+v", a.todoState)
	}

	a.advanceCanonicalTodo("move files")
	if a.todoState[1].Status != "completed" || a.todoState[2].Status != "in_progress" {
		t.Fatalf("completing a sub-step should promote its sibling: %+v", a.todoState)
	}

	a.advanceCanonicalTodo("fix imports")
	if a.todoState[2].Status != "completed" || a.todoState[0].Status != "in_progress" {
		t.Fatalf("after the last sub-step the phase should become the signable item: %+v", a.todoState)
	}

	a.advanceCanonicalTodo("Port the parser")
	if a.todoState[0].Status != "completed" || a.todoState[3].Status != "pending" || a.todoState[4].Status != "in_progress" {
		t.Fatalf("phase sign-off should promote the next phase's first sub-step: %+v", a.todoState)
	}
}
