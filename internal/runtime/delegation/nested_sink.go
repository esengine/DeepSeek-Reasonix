package delegation

import (
	"context"
	"reasonix/internal/runtime/agent"
	"strings"

	"reasonix/internal/contract/event"
)

// nestedSink is the event view a fleet or parallel_tasks item runs under: tool
// events are re-parented, usage is attributed to the sub-agent, everything else
// is dropped. It is a type rather than a func sink because optional sink
// capabilities are forwarded by method, and a bare func sink swallowed every
// delegation audit a fleet child produced.
type nestedSink struct {
	// Audits are accounting, not presentation: the embedded forwarder passes
	// every optional capability straight through to the parent.
	event.AuditForwarder
	parentID string
	parent   event.Sink
}

// RecordProtocolRecovery attributes a repair to the child it happened in. The
// producers run inside the child's own loop and hold no id; this sink is the
// layer that knows which run it is wrapping, as it already does for tools.
func (s nestedSink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	if a.ChildID == "" {
		a.ChildID = s.parentID
	}
	event.RecordProtocolRecovery(s.parent, a)
}

func (s nestedSink) Emit(e event.Event) {
	switch e.Kind {
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		e.Tool.ParentID = s.parentID
		e.Tool.ID = namespaceToolID(s.parentID, e.Tool.ID)
		// Re-parented work changes producer: in the parent's stream this is the
		// child's, whatever role the child plays in its own.
		e.Source = event.UsageSourceSubagent
		s.parent.Emit(e)
	case event.Usage:
		if e.UsageSource == "" {
			e.UsageSource = event.UsageSourceSubagent
		}
		s.parent.Emit(e)
	}
}

// namespaceToolID puts id under parentID, once. parallel_tasks and fleet
// pre-nest the sink they also hand to the call context, and RunProfileSpec
// nests again off that context, so this ran twice on the same id: the doubled
// prefix stops it starting with its parent's, which is what a frontend's
// dispatch-to-result matching and every trajectory reader key on.
func namespaceToolID(parentID, id string) string {
	if parentID == "" || strings.HasPrefix(id, parentID+"/") {
		return id
	}
	return parentID + "/" + id
}

// subSink is the nesting sink for the child of the call in ctx: the parent
// stream with tool ids re-parented under the parent call. Discard when there is
// no parent stream — a headless run loop, or a direct Execute in a test.
func subSink(ctx context.Context) event.Sink {
	parentID, parent, _, ok := agent.CallContext(ctx)
	if !ok || parent == nil {
		return event.Discard
	}
	return subSinkFor(parentID, parent)
}

// subSinkFor builds the nesting sink from an already-captured parent ID + stream,
// for the background path where the job runs under a context that no longer
// carries the call context. Falls back to Discard when there's no parent stream.
func subSinkFor(parentID string, parent event.Sink) event.Sink {
	if parent == nil {
		return event.Discard
	}
	return nestedSink{AuditForwarder: event.AuditForwarder{Inner: parent}, parentID: parentID, parent: parent}
}
