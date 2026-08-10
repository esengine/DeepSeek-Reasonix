package agent

import (
	"reasonix/internal/event"
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

func (s nestedSink) Emit(e event.Event) {
	switch e.Kind {
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		e.Tool.ParentID = s.parentID
		e.Tool.ID = s.parentID + "/" + e.Tool.ID
		s.parent.Emit(e)
	case event.Usage:
		if e.UsageSource == "" {
			e.UsageSource = event.UsageSourceSubagent
		}
		s.parent.Emit(e)
	}
}
