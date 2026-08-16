package agent

import (
	"encoding/json"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// recordToolReceipts files the turn-scoped evidence for one executed call:
// always the model-visible call for audit, plus the real target's attributes
// for mutation/read classification when a proxy resolved elsewhere.
func (a *Agent) finalizeObservedToolReceipts(plan *toolCallPlan, result string, execution *tool.ShellExecution, err error) {
	a.observeAfterMutation(plan)
	plan.mutationAfterDone = true
	a.recordToolReceipts(plan, result, execution, err)
}

// emitTodoResultPreview flips the todo_write card to done the moment the call
// executes without publishing a second terminal result. Batch ToolResult
// events still wait for the whole provider batch and remain the only terminal
// events observed by append-only sinks.
func (a *Agent) emitTodoResultPreview(call provider.ToolCall, output string) {
	if a == nil || a.svc.sink == nil {
		return
	}
	a.svc.sink.Emit(event.Event{
		Kind: event.ToolResultPreview,
		Tool: event.Tool{ID: call.ID, Name: call.Name, Args: call.Arguments, ReadOnly: true, Output: output},
	})
}

func (a *Agent) recordToolReceipts(plan *toolCallPlan, result string, execution *tool.ShellExecution, err error) {
	if a.task.ledger == nil {
		return
	}
	call := plan.call
	args := json.RawMessage(call.Arguments)
	switch {
	case call.Name == "complete_step":
		rec := evidence.ReceiptFromToolCall(call.Name, args, err == nil, plan.readOnly)
		a.task.ledger.Record(rec)
		a.commitToolReceipt(rec)
		if err == nil {
			a.advanceCanonicalTodo(rec.Step)
		}
	case plan.evidenceName != call.Name:
		proxy := evidence.ReceiptFromToolCall(call.Name, args, err == nil, true)
		a.task.ledger.Record(proxy)
		rec := evidence.ReceiptFromToolCall(plan.evidenceName, plan.evidenceArgs, err == nil, plan.readOnly)
		rec.Mutation = plan.effects.ContentMutation
		decorateExecutionReceipt(&rec, result, execution)
		a.task.ledger.Record(rec)
		a.commitToolReceipt(rec)
	default:
		rec := evidence.ReceiptFromToolCall(call.Name, args, err == nil, plan.tool.ReadOnly())
		rec.Mutation = plan.effects.ContentMutation
		decorateExecutionReceipt(&rec, result, execution)
		a.task.ledger.Record(rec)
		a.commitToolReceipt(rec)
		if err == nil && call.Name == "todo_write" {
			a.setTodoState(rec.Todos)
			if len(rec.Todos) > 0 {
				a.turn.deliveryCriteriaEstablished = true
			}
			a.emitTodoResultPreview(call, result)
		}
	}
}
