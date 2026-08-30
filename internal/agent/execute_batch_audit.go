package agent

import (
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func (a *Agent) emitBatchToolResults(calls []provider.ToolCall, outcomes []toolOutcome, durations, startedAt []int64, ranParallel []bool, batchStart time.Time) {
	for i, c := range calls {
		o := outcomes[i]
		t, _, ambiguous := a.svc.tools.ResolveCall(c.Name)
		ok := t != nil && len(ambiguous) == 0
		readOnly := ok && t.ReadOnly()
		if c.ResolvedReadOnly != nil {
			readOnly = *c.ResolvedReadOnly
		}
		tr := event.Tool{
			ID:           c.ID,
			Name:         c.Name,
			Args:         c.Arguments,
			ResolvedName: c.ResolvedName,
			CapabilityID: c.CapabilityID,
			Output:       o.output,
			Err:          o.errMsg,
			ReadOnly:     readOnly,
			Truncated:    o.truncated,
			DurationMs:   durations[i],
			Execution:    toEventShellExecution(o.execution, durations[i]),
		}
		if startedAt[i] > 0 {
			tr.StartedAt = startedAt[i]
			tr.EndedAt = startedAt[i] + durations[i]
			if mutation := o.workspaceMutation; mutation != nil {
				tr.WorkspaceMutation = true
				tr.WorkspacePaths = append([]string(nil), mutation.Paths...)
				tr.WorkspaceAllPaths = mutation.AllPaths
			}
		}
		a.svc.sink.Emit(event.Event{Kind: event.ToolResult, Tool: tr})
		if o.truncated && o.truncMsg != "" {
			a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: o.truncMsg})
		}
		a.recordToolExecutionAudit(readOnly, ranParallel[i], startedAt[i], durations[i], batchStart, o)
	}
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
