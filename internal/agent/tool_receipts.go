package agent

import (
	"context"
	"encoding/json"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/taskcontract"
	"reasonix/internal/tool"
)

// recordToolReceipts files the turn-scoped evidence for one executed call:
// always the model-visible call for audit, plus the real target's attributes
// for mutation/read classification when a proxy resolved elsewhere.
func (a *Agent) finalizeObservedToolReceipts(plan *toolCallPlan, result string, execution *tool.ShellExecution, err error) string {
	a.observeAfterMutation(plan)
	plan.mutationAfterDone = true
	return a.recordToolReceipts(plan, result, execution, err)
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

func (a *Agent) recordToolReceipts(plan *toolCallPlan, result string, execution *tool.ShellExecution, err error) string {
	if a.task.ledger == nil {
		return result
	}
	call := plan.call
	args := json.RawMessage(call.Arguments)
	var repairedDeferred []evidence.TodoItem
	if err == nil && call.Name == "todo_write" {
		if deferred, ok := normalizeRepairedTodoArgs(plan.cctx, args); ok {
			repairedDeferred = deferred
		}
	}
	// The session floor in force at write time is a fact of the write: it
	// rides the receipt so the per-turn contract replay re-derives the same
	// floor obligations even after the floor changes.
	floorStamp := a.turn.constraints.PolicyFloor.String()
	if floorStamp == taskcontract.PolicyFloorNone.String() {
		floorStamp = ""
	}
	switch {
	case call.Name == "complete_step":
		rec := evidence.ReceiptFromToolCall(call.Name, args, err == nil, plan.readOnly)
		a.stampReceiptDeliveryScope(&rec)
		rec.PolicyFloor = floorStamp
		a.task.ledger.Record(rec)
		a.commitToolReceipt(rec)
		if err == nil {
			transition := a.advanceCanonicalTodo(rec.Step)
			if len(transition.consumed) > 0 {
				result = appendDeferredAppliedFeedback(result, transition.consumed, transition.todos)
			}
			plan.hostTodoState = a.hostTodoStateSnapshot(false)
		}
	case plan.evidenceName != call.Name:
		proxy := evidence.ReceiptFromToolCall(call.Name, args, err == nil, true)
		proxy.ToolCallID = call.ID
		a.task.ledger.Record(proxy)
		rec := evidence.ReceiptFromToolCall(plan.evidenceName, plan.evidenceArgs, err == nil, plan.readOnly)
		rec.ToolCallID = call.ID
		rec.Mutation = plan.effects.ContentMutation
		a.stampReceiptDeliveryScope(&rec)
		rec.PolicyFloor = floorStamp
		decorateExecutionReceipt(&rec, result, execution)
		a.task.ledger.Record(rec)
		a.commitToolReceipt(rec)
	default:
		rec := evidence.ReceiptFromToolCall(call.Name, args, err == nil, plan.tool.ReadOnly())
		rec.ToolCallID = call.ID
		rec.Mutation = plan.effects.ContentMutation
		a.stampReceiptDeliveryScope(&rec)
		rec.PolicyFloor = floorStamp
		decorateExecutionReceipt(&rec, result, execution)
		if err == nil && call.Name == "todo_write" {
			transition := a.acceptTodoUpdate(rec.Todos, repairedDeferred, plan.planReplacementAuthorized || a.planMode.Load())
			rec.Todos = transition.todos
			plan.hostTodoState = a.hostTodoStateSnapshot(true)
			if len(transition.added) > 0 && !strings.Contains(strings.ToLower(result), "recorded") {
				result = appendDeferredRecordedFeedback(result, transition.added, transition.todos)
			}
			if len(transition.consumed) > 0 {
				result = appendDeferredAppliedFeedback(result, transition.consumed, transition.todos)
			}
			if len(rec.Todos) > 0 {
				a.turn.deliveryCriteriaEstablished = true
			}
		}
		a.task.ledger.Record(rec)
		a.commitToolReceipt(rec)
		if err == nil && call.Name == "todo_write" {
			a.emitTodoResultPreview(call, result)
		}
	}
	return result
}

func normalizeRepairedTodoArgs(ctx context.Context, args json.RawMessage) ([]evidence.TodoItem, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return nil, false
	}
	rawTodos, ok := fields["todos"]
	if !ok {
		return nil, false
	}
	var next []evidence.TodoItem
	if err := json.Unmarshal(rawTodos, &next); err != nil {
		return nil, false
	}
	previous := []evidence.TodoItem(nil)
	if ledger, ok := evidence.FromContext(ctx); ok {
		if prior, found := ledger.LatestTodos(); found && len(prior) > 0 {
			previous = prior
		}
	}
	if len(previous) == 0 {
		previous, _ = evidence.TodoStateFromContext(ctx)
	}
	_, deferred, repaired := evidence.RepairSerialTodoUpdateWithDeferred(previous, next)
	if !repaired {
		return nil, false
	}
	return deferred, true
}

func appendDeferredRecordedFeedback(result string, ids []string, todos []evidence.TodoItem) string {
	names := todoNamesForKeys(ids, todos)
	if names == "" {
		names = "the later task(s)"
	}
	return strings.TrimRight(result, "\n") + " The completion report for " + names + " was recorded and deferred while an earlier serial task remains in progress. This is not an error. Do not submit again; it will be applied automatically after the preceding tasks complete."
}

func appendDeferredAppliedFeedback(result string, ids []string, todos []evidence.TodoItem) string {
	names := todoNamesForKeys(ids, todos)
	if names == "" {
		names = "the previously deferred task(s)"
	}
	return strings.TrimRight(result, "\n") + " Todo list updated. Previously deferred completions for " + names + " were applied automatically. No additional todo update is needed for these tasks."
}
