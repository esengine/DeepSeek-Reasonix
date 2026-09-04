package agent

import (
	"context"
	"encoding/json"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// recordToolReceipts files the turn-scoped evidence for one executed call:
// always the model-visible call for audit, plus the real target's attributes
// for mutation/read classification when a proxy resolved elsewhere.
func (a *Agent) recordToolReceipts(ctx context.Context, plan *toolCallPlan, result string, execution *tool.ShellExecution, err error) {
	if a.task.ledger == nil {
		return
	}
	call := plan.call
	args := json.RawMessage(call.Arguments)
	switch {
	case call.Name == "complete_step":
		rec := evidence.ReceiptFromToolCall(call.Name, args, err == nil, plan.facts())
		a.task.ledger.Record(rec)
		if err == nil {
			a.advanceCanonicalTodo(rec.Step)
		}
	case plan.evidenceName != call.Name:
		a.task.ledger.Record(evidence.ReceiptFromToolCall(call.Name, args, err == nil, evidence.ToolFacts{ReadOnly: true}))
		rec := evidence.ReceiptFromToolCall(plan.evidenceName, plan.evidenceArgs, err == nil, plan.facts())
		decorateExecutionReceipt(&rec, result, execution)
		decorateObservedPaths(&rec, plan)
		a.settleUnchangedWorkspace(ctx, &rec, plan)
		a.reviewCoverageOf(&rec, plan, result)
		a.task.ledger.Record(rec)
	default:
		rec := evidence.ReceiptFromToolCall(call.Name, args, err == nil, plan.facts())
		decorateExecutionReceipt(&rec, result, execution)
		decorateObservedPaths(&rec, plan)
		a.settleUnchangedWorkspace(ctx, &rec, plan)
		a.reviewCoverageOf(&rec, plan, result)
		a.task.ledger.Record(rec)
		if err == nil && call.Name == "todo_write" {
			before := a.CanonicalTodoState()
			a.setTodoState(rec.Todos)
			a.observeTodoTransition(before, a.CanonicalTodoState())
			if len(rec.Todos) > 0 {
				a.turn.deliveryCriteriaEstablished = true
			}
		}
	}
}

// notifyToolHooks lets success and failure hooks observe a finished call. The
// name is the real target's, so a proxied tool reaches hooks under the tool
// they were written against rather than under the proxy.
func (a *Agent) notifyToolHooks(ctx context.Context, name string, args json.RawMessage, result string, err error) {
	if a.svc.hooks == nil {
		return
	}
	if err != nil {
		a.svc.hooks.PostToolUseFailure(ctx, name, args, result, err)
		return
	}
	a.svc.hooks.PostToolUse(ctx, name, args, result)
}
