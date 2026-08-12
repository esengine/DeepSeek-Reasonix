package agent

import (
	"encoding/json"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// recordToolReceipts files the turn-scoped evidence for one executed call:
// always the model-visible call for audit, plus the real target's attributes
// for mutation/read classification when a proxy resolved elsewhere.
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
		if err == nil {
			a.advanceCanonicalTodo(rec.Step)
		}
	case plan.evidenceName != call.Name:
		a.task.ledger.Record(evidence.ReceiptFromToolCall(call.Name, args, err == nil, true))
		rec := evidence.ReceiptFromToolCall(plan.evidenceName, plan.evidenceArgs, err == nil, plan.readOnly)
		decorateExecutionReceipt(&rec, result, execution)
		a.task.ledger.Record(rec)
	default:
		rec := evidence.ReceiptFromToolCall(call.Name, args, err == nil, plan.tool.ReadOnly())
		decorateExecutionReceipt(&rec, result, execution)
		a.task.ledger.Record(rec)
		if err == nil && call.Name == "todo_write" {
			a.setTodoState(rec.Todos)
			if len(rec.Todos) > 0 {
				a.deliveryCriteriaEstablished = true
			}
		}
	}
}
