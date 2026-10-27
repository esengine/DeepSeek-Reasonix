package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// nestedInvoker lets a running tool make calls that pass every stage a
// model-issued call does. Each shows as its own card under an id derived from
// the caller's; its result goes to the calling tool, not the conversation.
type nestedInvoker struct {
	a      *Agent
	turn   *turnRuntime
	parent *toolCallPlan
	seq    atomic.Int32
}

func (n *nestedInvoker) Invoke(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	call := provider.ToolCall{ID: tool.NestedCallID(n.parent.call.ID, int(n.seq.Add(1))), Name: name, Arguments: string(args)}
	call = n.a.prepareCallArguments(call)
	n.a.emitFullToolDispatch(ctx, call, false)
	started := time.Now()
	out := n.a.executeOne(tool.MarkNested(ctx), n.turn, call)
	elapsed := time.Since(started).Milliseconds()
	resultEvent := event.Tool{
		ID: call.ID, Name: name, Args: call.ExecutionArguments(), ArgumentNormalization: call.ArgumentNormalization, Output: out.output, Err: out.errMsg,
		RefusalCode: out.refusalCode, Bound: out.bound, DurationMs: elapsed,
		WorkspaceLease: out.workspaceLease,
		StartedAt:      started.UnixMilli(), EndedAt: started.UnixMilli() + elapsed, Issuer: event.IssuedByModel,
		Executed: out.executed,
	}
	attachWorkspaceMutation(&resultEvent, out.workspaceMutation)
	n.a.svc.sink.Emit(event.Event{Kind: event.ToolResult, Tool: resultEvent})
	// The bound fits a result to the model's context; the calling tool reads
	// the result itself, and a window or spill pointer in its place is not it.
	result := out.output
	if out.bound.Kind != event.BoundWhole {
		result = out.unbounded
	}
	if out.errMsg != "" || out.blocked || out.refusalCode != "" {
		reason := out.errMsg
		if reason == "" {
			reason = firstLine(out.output)
		}
		return result, fmt.Errorf("%w: %s: %s", tool.ErrNestedCallFailed, name, reason)
	}
	return result, nil
}
