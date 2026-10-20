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

var _ tool.BatchInvoker = (*nestedInvoker)(nil)

func (n *nestedInvoker) Invoke(ctx context.Context, name string, args json.RawMessage) (string, error) {
	r := n.InvokeResult(ctx, tool.NestedCall{Name: name, Args: args})
	return r.Output, r.Err
}

func (n *nestedInvoker) InvokeResult(ctx context.Context, c tool.NestedCall) tool.NestedResult {
	return n.run(ctx, n.next(c))
}

// InvokeMany runs calls concurrently only when every one of them could share a
// model's parallel batch; otherwise they run in order, as written.
func (n *nestedInvoker) InvokeMany(ctx context.Context, calls []tool.NestedCall) []tool.NestedResult {
	planned := make([]provider.ToolCall, len(calls))
	parallel := true
	for i, c := range calls {
		planned[i] = n.next(c)
		parallel = parallel && parallelisable(ctx, n.a.svc.tools, planned[i])
	}
	out := make([]tool.NestedResult, len(calls))
	if parallel && len(calls) > 1 {
		ran := runParallel(ctx, 0, len(calls), func(i int) { out[i] = n.run(ctx, planned[i]) })
		for i := ran; i < len(calls); i++ {
			out[i] = tool.NestedResult{Err: fmt.Errorf("%w: %s: %w", tool.ErrNestedCallFailed, calls[i].Name, context.Cause(ctx))}
		}
		return out
	}
	for i := range planned {
		out[i] = n.run(ctx, planned[i])
	}
	return out
}

func (n *nestedInvoker) next(c tool.NestedCall) provider.ToolCall {
	args := c.Args
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return provider.ToolCall{ID: tool.NestedCallID(n.parent.call.ID, int(n.seq.Add(1))), Name: c.Name, Arguments: string(args)}
}

func (n *nestedInvoker) run(ctx context.Context, call provider.ToolCall) tool.NestedResult {
	n.a.emitFullToolDispatch(ctx, call, false)
	started := time.Now()
	out := n.a.executeOne(tool.MarkNested(ctx), n.turn, call)
	elapsed := time.Since(started).Milliseconds()
	n.a.svc.sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: call.ID, Name: call.Name, Args: call.Arguments, Output: out.output, Err: out.errMsg,
		RefusalCode: out.refusalCode, Bound: out.bound, DurationMs: elapsed,
		WorkspaceLease: out.workspaceLease,
		StartedAt:      started.UnixMilli(), EndedAt: started.UnixMilli() + elapsed, Issuer: event.IssuedByModel,
		Executed: out.executed,
	}})
	// The bound fits a result to the model's context; the calling tool reads
	// the result itself, and a window or spill pointer in its place is not it.
	result := tool.NestedResult{Output: out.output, ResultRef: out.resultRef}
	if out.bound.Kind != event.BoundWhole {
		result.Output = out.unbounded
	}
	if out.errMsg != "" || out.blocked || out.refusalCode != "" {
		reason := out.errMsg
		if reason == "" {
			reason = firstLine(out.output)
		}
		result.Err = fmt.Errorf("%w: %s: %s", tool.ErrNestedCallFailed, call.Name, reason)
	}
	return result
}
