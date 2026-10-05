package agent

// When a plan card is drawn. complete_step advances the task list and emits
// that card itself, so a todo_write restating the same list draws nothing —
// the call still runs and is still recorded, only the second telling is cut.

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
)

// emitEagerToolDispatch draws a call's card up front, where a running state is
// worth showing. todo_write returns instantly and has none.
func (a *Agent) emitEagerToolDispatch(ctx context.Context, c provider.ToolCall) {
	if c.Name == "todo_write" {
		return
	}
	a.emitFullToolDispatch(ctx, c, false)
}

// emitToolCard draws the finished card, opening one first for the calls held
// back above so a frontend still sees a dispatch before its result.
func (a *Agent) emitToolCard(ctx context.Context, c provider.ToolCall, tr event.Tool, echo bool) {
	if echo {
		return
	}
	if c.Name == "todo_write" {
		a.emitFullToolDispatch(ctx, c, false)
	}
	a.svc.sink.Emit(event.Event{Kind: event.ToolResult, Tool: tr})
}

// todoStateBefore snapshots the task list a todo_write is about to overwrite.
// Taken before the call runs, because recording its receipt is what makes the
// canonical list equal to the submitted one.
func (a *Agent) todoStateBefore(c provider.ToolCall) []evidence.TodoItem {
	if c.Name != "todo_write" {
		return nil
	}
	return a.CanonicalTodoState()
}

// todoWriteEchoes reports whether a successful todo_write left the task list
// exactly as it found it.
func (a *Agent) todoWriteEchoes(c provider.ToolCall, before []evidence.TodoItem, errMsg string) bool {
	if c.Name != "todo_write" || errMsg != "" || len(before) == 0 {
		return false
	}
	rec := evidence.ReceiptFromToolCall(c.Name, json.RawMessage(c.Arguments), true, evidence.ToolFacts{ReadOnly: true})
	return evidence.SameTodos(before, rec.Todos)
}
