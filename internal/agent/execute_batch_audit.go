package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func (a *Agent) emitBatchToolResult(ctx context.Context, c provider.ToolCall, o toolOutcome, committedMessage provider.Message, duration, started int64, parallel bool, batchStart time.Time) error {
	t, _, ambiguous := a.svc.tools.ResolveCall(c.Name)
	ok := t != nil && len(ambiguous) == 0
	readOnly := ok && t.ReadOnly()
	if c.ResolvedReadOnly != nil {
		readOnly = *c.ResolvedReadOnly
	}
	tr := event.Tool{
		RunState:       outcomeRunState(o),
		ID:             c.ID,
		Name:           c.Name,
		Args:           c.Arguments,
		ResolvedName:   c.ResolvedName,
		CapabilityID:   c.CapabilityID,
		Output:         o.output,
		Err:            o.errMsg,
		ReadOnly:       readOnly,
		Truncated:      o.truncated,
		DurationMs:     duration,
		Execution:      toEventShellExecution(o.execution, duration),
		PresentedFiles: append([]provider.PresentedFile(nil), o.presentedFiles...),
	}
	if o.diagnostic != nil {
		tr.Diagnostic, _ = json.Marshal(o.diagnostic)
	}
	if o.subagentOutcome != nil {
		tr.SubagentRef = o.subagentOutcome.Ref
		tr.SubagentStatus = string(o.subagentOutcome.Status)
		tr.SubagentErrorCode = o.subagentOutcome.ErrorCode
		tr.SubagentRetryable = o.subagentOutcome.Retryable
	} else if isSubagentToolCall(c) {
		if outcome, ok := ParseSubagentOutcome(o.output); ok {
			tr.SubagentRef = outcome.Ref
			tr.SubagentStatus = string(outcome.Status)
			tr.SubagentErrorCode = outcome.ErrorCode
			tr.SubagentRetryable = outcome.Retryable
		}
	}
	var committedTodos []evidence.TodoItem
	if c.Name == "todo_write" && o.errMsg == "" && !o.blocked {
		receipt := evidence.ReceiptFromToolCall("todo_write", json.RawMessage(c.Arguments), true, true)
		// Successful execution means the strict todo_write validator already
		// accepted these arguments. Commit the normalized call data itself; tool
		// output is presentation and may be compacted independently.
		committedTodos = append([]evidence.TodoItem(nil), receipt.Todos...)
		for i := range committedTodos {
			committedTodos[i].Content = strings.TrimSpace(committedTodos[i].Content)
		}
		tr.TodoWritten = true
		tr.Todos = make([]event.Todo, len(committedTodos))
		for i, todo := range committedTodos {
			tr.Todos[i] = event.Todo{Content: todo.Content, Status: todo.Status}
		}
	}
	if started > 0 {
		tr.StartedAt = started
		tr.EndedAt = started + duration
		if mutation := o.workspaceMutation; mutation != nil {
			tr.WorkspaceMutation = true
			tr.WorkspacePaths = append([]string(nil), mutation.Paths...)
			tr.WorkspaceAllPaths = mutation.AllPaths
		}
	}
	if err := event.EmitChecked(a.svc.sink, event.Event{Kind: event.ToolResult, MessageID: messageIdentity(ctx), Tool: tr, CommittedMessage: &committedMessage}); err != nil {
		return err
	}
	if tr.TodoWritten {
		a.setTodoState(committedTodos)
	}
	if o.truncated && o.truncMsg != "" {
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: o.truncMsg})
	}
	a.recordToolExecutionAudit(readOnly, parallel, started, duration, batchStart, o)
	return nil
}

func isSubagentToolName(name string) bool {
	switch name {
	case "task", "read_only_task", "run_skill", "read_only_skill", "explore", "research", "review", "security_review", "security-review", "parallel_tasks", "fleet":
		return true
	default:
		return false
	}
}

func isSubagentToolCall(call provider.ToolCall) bool {
	return isSubagentToolName(call.Name) || strings.HasPrefix(strings.TrimSpace(call.CapabilityID), "skill:")
}

func (a *Agent) recordToolExecutionAudit(readOnly, parallel bool, startedAt, durationMs int64, batchStart time.Time, o toolOutcome) {
	if a == nil || a.capabilityAudit == nil || startedAt <= 0 {
		return
	}
	queueMs := max(startedAt-batchStart.UnixMilli(), 0)
	rawBytes := len(o.output)
	if o.rawOutput != "" {
		rawBytes = len(o.rawOutput)
	}
	a.capabilityAudit.RecordToolExecution(readOnly, parallel, queueMs, durationMs, rawBytes, len(o.output))
}

func (a *Agent) buildBatchToolResult(ctx context.Context, call provider.ToolCall, o toolOutcome) provider.Message {
	state := outcomeRunState(o)
	msg := provider.Message{Role: provider.RoleTool, Content: o.output, Images: o.images, VisionSummary: o.visionSummary, ToolCallID: call.ID, Name: call.Name, ToolRunState: state, ToolExecution: toProviderToolExecution(o.execution), PresentedFiles: provider.NewPresentedFilesMetadata(o.presentedFiles)}
	if len(o.images) > 0 && a.imageResolver != nil {
		if inputs, err := a.imageResolver.PersistToolImages(ctx, o.images); err == nil && len(inputs) > 0 {
			msg.ImageInputs = inputs
			msg.Images = nil
		} else if err != nil {
			msg.Images = nil
			msg.Content += "\n[Image persistence failed. The tool already executed; its text result remains valid. Do not repeat the original action to retry image processing.]"
			msg.ToolDiagnostic, _ = json.Marshal(map[string]string{"code": "image_persistence_failed", "message": "Image persistence failed; completed tool text was preserved."})
		}
	}
	if o.diagnostic != nil && len(msg.ToolDiagnostic) == 0 {
		msg.ToolDiagnostic, _ = json.Marshal(o.diagnostic)
	}
	if o.rawOutput != "" && o.rawOutput != o.output {
		msg.RawContent = o.rawOutput
	}
	if env, ok := a.finalizedReadEnvelope(ctx, call, o); ok {
		if env.HasMore && len(msg.ToolDiagnostic) == 0 {
			msg.ToolDiagnostic, _ = json.Marshal(tool.OperationDiagnostic{Code: tool.ReadPartial, Path: env.Source.CanonicalPath, OperationID: call.ID, ActualSnapshot: env.Source.Snapshot, RequiredRanges: env.DeliveredRanges, Recovery: "continue with the next window only if the task requires more coverage"})
		}
		if raw, err := json.Marshal(env); err == nil {
			msg.ReadResult = raw
		}
	}
	if msg.ID == "" {
		msg.ID = NewMessageID()
	}
	return msg
}
