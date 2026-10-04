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
	call := provider.ToolCall{ID: fmt.Sprintf("%s.%d", n.parent.call.ID, n.seq.Add(1)), Name: name, Arguments: string(args)}
	n.a.emitFullToolDispatch(ctx, call, false)
	started := time.Now()
	out := n.a.executeOne(tool.MarkNested(ctx), n.turn, call)
	elapsed := time.Since(started).Milliseconds()
	n.a.svc.sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: call.ID, Name: name, Args: call.Arguments, Output: out.output, Err: out.errMsg,
		RefusalCode: out.refusalCode, Bound: out.bound, DurationMs: elapsed,
		WorkspaceLease: out.workspaceLease,
		StartedAt:      started.UnixMilli(), EndedAt: started.UnixMilli() + elapsed, Issuer: event.IssuedByModel,
		Executed: out.executed,
	}})
	if out.provenance.External() && !n.parent.nestedOrigin.External() {
		n.parent.nestedOrigin = out.provenance
	}
	if out.errMsg != "" || out.blocked || out.refusalCode != "" {
		reason := out.errMsg
		if reason == "" {
			reason = firstLine(out.output)
		}
		return out.output, fmt.Errorf("%w: %s: %s", tool.ErrNestedCallFailed, name, reason)
	}
	return out.output, nil
}
