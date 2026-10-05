package agent

// The host's canonical task list: the state that outlives a turn because it
// never rides in the prompt, so a later turn still sees an unfinished plan.

import (
	"encoding/json"
	"strings"

	"reasonix/internal/safety/evidence"
)

// SeedTodoState initializes the canonical task list from a host-generated
// starter list, such as an approved plan. A new host seed replaces stale state
// from earlier work so complete_step matches the plan the UI just displayed.
func (a *Agent) SeedTodoState(todos []evidence.TodoItem) {
	if len(todos) == 0 {
		return
	}
	before := a.CanonicalTodoState()
	a.setTodoState(todos)
	a.observeTodoTransition(before, a.CanonicalTodoState())
}

// ReplaceTodoState mirrors a host-generated todo list into the canonical state.
// It is used when the host, rather than the model, owns the full state transition.
func (a *Agent) ReplaceTodoState(todos []evidence.TodoItem) {
	before := a.CanonicalTodoState()
	a.setTodoState(todos)
	a.recordTodoState(a.CanonicalTodoState())
	a.observeTodoTransition(before, a.CanonicalTodoState())
}

// CanonicalTodoState returns a copy of the host-reconstructed task list.
func (a *Agent) CanonicalTodoState() []evidence.TodoItem {
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()
	return append([]evidence.TodoItem(nil), a.sess.todoState...)
}

func (a *Agent) incompleteCanonicalTodos() ([]evidence.TodoStepMatch, bool) {
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()
	if len(a.sess.todoState) == 0 {
		return nil, false
	}
	return evidence.IncompleteTodos(a.sess.todoState), true
}

func (a *Agent) hasIncompleteCanonicalCriteria() bool {
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()
	return len(a.sess.todoState) > 0 && len(evidence.IncompleteTodos(a.sess.todoState)) > 0
}

func (a *Agent) advanceCanonicalTodo(step string) {
	a.sess.todoMu.Lock()
	if len(a.sess.todoState) == 0 {
		a.sess.todoMu.Unlock()
		return
	}
	before := append([]evidence.TodoItem(nil), a.sess.todoState...)
	m, ok := evidence.MatchStep(step, a.sess.todoState)
	if !ok {
		a.sess.todoMu.Unlock()
		return
	}
	if !evidence.AdvanceSerialTodo(a.sess.todoState, m.Index-1) {
		a.sess.todoMu.Unlock()
		// A sign-off the list did not move for is a renewal. Nothing about the
		// list records it, which is what made it read as an advance.
		a.observeTodoKind(evidence.TodoRenewal, before)
		return
	}
	snapshot := append([]evidence.TodoItem(nil), a.sess.todoState...)
	a.sess.todoMu.Unlock()
	a.recordTodoState(snapshot)
	a.emitTodoState(snapshot, m.Index)
	a.observeTodoTransition(before, snapshot)
}

// recordTodoState logs the host-advanced list as a synthetic todo_write receipt
// so the per-turn final gate (which reads the ledger's latest todo_write) sees
// the advance — the model no longer has to re-send a todo_write to mark the
// completion. It bypasses the todo_write tool, so the completion-transition
// guard never runs on it.
func (a *Agent) recordTodoState(todos []evidence.TodoItem) {
	if a.task.ledger == nil {
		return
	}
	args, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		return
	}
	a.task.ledger.Record(evidence.ReceiptFromToolCall("todo_write", json.RawMessage(args), true, evidence.ToolFacts{ReadOnly: true}))
}

func canonicalTodoStatus(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "pending"
	}
	return s
}
