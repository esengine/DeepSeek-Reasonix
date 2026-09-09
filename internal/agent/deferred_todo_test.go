package agent

import (
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

func deferredTodoTestList(statuses ...string) []evidence.TodoItem {
	contents := []string{"A", "B", "C", "D", "E"}
	items := make([]evidence.TodoItem, 0, len(statuses))
	for i, status := range statuses {
		items = append(items, evidence.TodoItem{Content: contents[i], Status: status, StepID: string(rune('a' + i))})
	}
	return items
}

func todoStatuses(todos []evidence.TodoItem) []string {
	statuses := make([]string, 0, len(todos))
	for _, todo := range todos {
		statuses = append(statuses, todo.Status)
	}
	return statuses
}

func TestDeferredTodoUpdateIsIdempotentAndKeepsCanonicalSerial(t *testing.T) {
	previous := deferredTodoTestList("in_progress", "pending", "pending", "pending")
	next := deferredTodoTestList("in_progress", "completed", "completed", "pending")
	canonical, candidates, ok := evidence.RepairSerialTodoUpdateWithDeferred(previous, next)
	if !ok {
		t.Fatal("out-of-order completion should be repaired")
	}
	a := &Agent{}
	a.setTodoState(previous)
	for i := 0; i < 3; i++ {
		transition := a.acceptTodoUpdate(canonical, candidates, false)
		if err := evidence.ValidateSerialTodos(transition.todos); err != nil {
			t.Fatalf("iteration %d produced invalid canonical state: %v", i+1, err)
		}
		if !slices.Equal(transition.deferred, []string{"b", "c"}) {
			t.Fatalf("iteration %d deferred = %v, want [b c]", i+1, transition.deferred)
		}
		if i == 0 && !slices.Equal(transition.added, []string{"b", "c"}) {
			t.Fatalf("first transition added = %v, want [b c]", transition.added)
		}
		if i > 0 && len(transition.added) != 0 {
			t.Fatalf("duplicate transition added new records: %v", transition.added)
		}
	}
	if got := todoStatuses(a.CanonicalTodoState()); !slices.Equal(got, []string{"in_progress", "pending", "pending", "pending"}) {
		t.Fatalf("canonical statuses = %v, want serial pending suffix", got)
	}
}

func TestDeferredTodoConsumptionStopsAtFirstHole(t *testing.T) {
	a := &Agent{}
	a.setTodoState(deferredTodoTestList("completed", "in_progress", "pending", "pending", "pending"))
	a.sess.todoMu.Lock()
	a.sess.deferredTodoCompletions = map[string]deferredTodoCompletion{
		"b": {level: 0},
		"c": {level: 0},
		"e": {level: 0},
	}
	a.sess.todoMu.Unlock()

	transition := a.acceptTodoUpdate(a.CanonicalTodoState(), nil, false)
	if !slices.Equal(transition.consumed, []string{"b", "c"}) {
		t.Fatalf("consumed = %v, want [b c]", transition.consumed)
	}
	if !slices.Equal(transition.deferred, []string{"e"}) {
		t.Fatalf("deferred after hole = %v, want [e]", transition.deferred)
	}
	if got := todoStatuses(transition.todos); !slices.Equal(got, []string{"completed", "completed", "completed", "in_progress", "pending"}) {
		t.Fatalf("canonical after hole = %v, want D current and E pending", got)
	}
	if err := evidence.ValidateSerialTodos(transition.todos); err != nil {
		t.Fatalf("canonical after hole is invalid: %v", err)
	}
}

func TestDeferredTodoExtremeCompletionOrderEventuallyCompletesInSerialOrder(t *testing.T) {
	a := &Agent{}
	a.setTodoState(deferredTodoTestList("in_progress", "pending", "pending", "pending", "pending"))

	for _, index := range []int{4, 2, 1} { // E -> C -> B
		previous := a.CanonicalTodoState()
		next := append([]evidence.TodoItem(nil), previous...)
		next[index].Status = "completed"
		canonical, candidates, ok := evidence.RepairSerialTodoUpdateWithDeferred(previous, next)
		if !ok {
			t.Fatalf("step %d out-of-order completion was not repaired", index+1)
		}
		transition := a.acceptTodoUpdate(canonical, candidates, false)
		if err := evidence.ValidateSerialTodos(transition.todos); err != nil {
			t.Fatalf("after step %d canonical invalid: %v", index+1, err)
		}
	}
	if got := a.DeferredTodoCompletions(); !slices.Equal(got, []string{"b", "c", "e"}) {
		t.Fatalf("deferred before A = %v, want [b c e]", got)
	}

	previous := a.CanonicalTodoState()
	next := append([]evidence.TodoItem(nil), previous...)
	next[0].Status = "completed"
	next[1].Status = "in_progress"
	transition := a.acceptTodoUpdate(next, nil, false)
	if !slices.Equal(transition.consumed, []string{"b", "c"}) {
		t.Fatalf("after A consumed = %v, want [b c]", transition.consumed)
	}
	if got := todoStatuses(transition.todos); !slices.Equal(got, []string{"completed", "completed", "completed", "in_progress", "pending"}) {
		t.Fatalf("after A canonical = %v, want D current", got)
	}

	previous = a.CanonicalTodoState()
	next = append([]evidence.TodoItem(nil), previous...)
	next[3].Status = "completed"
	next[4].Status = "in_progress"
	transition = a.acceptTodoUpdate(next, nil, false)
	if !slices.Equal(transition.consumed, []string{"e"}) {
		t.Fatalf("after D consumed = %v, want [e]", transition.consumed)
	}
	if !slices.Equal(a.DeferredTodoCompletions(), nil) {
		t.Fatalf("deferred after D = %v, want empty", a.DeferredTodoCompletions())
	}
	if got := todoStatuses(transition.todos); !slices.Equal(got, []string{"completed", "completed", "completed", "completed", "completed"}) {
		t.Fatalf("final canonical = %v, want all completed", got)
	}
	if err := evidence.ValidateSerialTodos(transition.todos); err != nil {
		t.Fatalf("final canonical is invalid: %v", err)
	}
}

func TestDeferredTodoStateRestoresFromSessionTranscript(t *testing.T) {
	baseArgs := `{"todos":[{"content":"A","status":"in_progress","step_id":"a"},{"content":"B","status":"pending","step_id":"b"}]}`
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "todo_write", Arguments: baseArgs}}},
		{Role: provider.RoleTool, ToolCallID: "t1", Name: "todo_write", Content: "Todos updated", DeferredTodoCompletions: &provider.DeferredTodoCompletionState{Items: []provider.DeferredTodoCompletion{{ID: "b", Level: 0}}}},
	}
	a := &Agent{}
	a.rebuildTodoState(msgs)
	if got := a.DeferredTodoCompletions(); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("reloaded deferred = %v, want [b]", got)
	}
	if got := todoStatuses(a.CanonicalTodoState()); !slices.Equal(got, []string{"in_progress", "pending"}) {
		t.Fatalf("reloaded canonical = %v, want A current/B pending", got)
	}

	msgs = append(msgs,
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "complete_step", Arguments: `{"step":"A"}`}}},
		provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Name: "complete_step", Content: "signed off", DeferredTodoCompletions: &provider.DeferredTodoCompletionState{Items: []provider.DeferredTodoCompletion{}}},
	)
	a.rebuildTodoState(msgs)
	if got := a.DeferredTodoCompletions(); len(got) != 0 {
		t.Fatalf("cleared deferred after reload = %v, want empty", got)
	}
	if got := todoStatuses(a.CanonicalTodoState()); !slices.Equal(got, []string{"completed", "in_progress"}) {
		t.Fatalf("reloaded post-signoff canonical = %v, want B current", got)
	}
}

func TestDeferredTodoStateSurvivesSessionSaveAndReload(t *testing.T) {
	baseArgs := `{"todos":[{"content":"A","status":"in_progress","step_id":"a"},{"content":"B","status":"pending","step_id":"b"}]}`
	session := NewSession("")
	session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t1", Name: "todo_write", Arguments: baseArgs}}})
	session.Add(provider.Message{
		Role:                    provider.RoleTool,
		ToolCallID:              "t1",
		Name:                    "todo_write",
		Content:                 "recorded and deferred",
		DeferredTodoCompletions: &provider.DeferredTodoCompletionState{Items: []provider.DeferredTodoCompletion{{ID: "b", Level: 0}}},
	})
	path := filepath.Join(t.TempDir(), "todo.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatalf("save session: %v", err)
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	a := &Agent{sess: sessionRuntime{conversation: reloaded}}
	a.rebuildTodoState(reloaded.Snapshot())
	if got := a.DeferredTodoCompletions(); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("reloaded deferred = %v, want [b]", got)
	}
	next := a.CanonicalTodoState()
	next[0].Status = "completed"
	next[1].Status = "in_progress"
	transition := a.acceptTodoUpdate(next, nil, false)
	if !slices.Equal(transition.consumed, []string{"b"}) || len(transition.deferred) != 0 {
		t.Fatalf("post-reload consumption = consumed %v deferred %v, want b/empty", transition.consumed, transition.deferred)
	}
	if err := evidence.ValidateSerialTodos(transition.todos); err != nil {
		t.Fatalf("post-reload canonical invalid: %v", err)
	}
}

func TestDeferredTodoStateIgnoresStaleProgressAndClearsOnReplacement(t *testing.T) {
	a := &Agent{}
	a.setTodoState(deferredTodoTestList("completed", "in_progress", "pending"))
	a.sess.todoMu.Lock()
	a.sess.deferredTodoCompletions = map[string]deferredTodoCompletion{"c": {level: 0}}
	a.sess.todoMu.Unlock()

	stale := deferredTodoTestList("in_progress", "pending", "pending")
	transition := a.acceptTodoUpdate(stale, []evidence.TodoItem{stale[1]}, false)
	if got := todoStatuses(transition.todos); !slices.Equal(got, []string{"completed", "in_progress", "pending"}) {
		t.Fatalf("stale update regressed canonical = %v", got)
	}
	if !slices.Equal(a.DeferredTodoCompletions(), []string{"c"}) {
		t.Fatalf("stale update changed deferred = %v, want [c]", a.DeferredTodoCompletions())
	}

	replaceCanonicalTodoState(a, []evidence.TodoItem{{Content: "new", Status: "in_progress", StepID: "new"}})
	if got := a.DeferredTodoCompletions(); len(got) != 0 {
		t.Fatalf("plan replacement retained stale deferred = %v", got)
	}
}
