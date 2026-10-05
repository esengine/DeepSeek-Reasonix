package event

import "reasonix/internal/base/diff"

// MarkEmbeddedDiffs wraps sink so a finished shell tool result whose whole
// output is a unified diff carries OutputDiff; a frontend then renders it as a
// diff. enabled false is a pass-through. The tag is computed once here, at the
// one sink every emitter shares, so the agent's tool loop, the controller's
// synthetic calls, and sub-agents are tagged alike.
func MarkEmbeddedDiffs(sink Sink, enabled bool) Sink {
	if !enabled || sink == nil {
		return sink
	}
	return markDiffSink{inner: sink}
}

type markDiffSink struct{ inner Sink }

func (s markDiffSink) Emit(e Event) {
	// A result the host trimmed to fit context is not a whole diff any more, so
	// its lossy Bound keeps it out of the tag rather than showing a partial diff.
	if e.Kind == ToolResult && e.Tool.Execution != nil && e.Tool.Err == "" && !e.Tool.Bound.Lossy() && !e.Tool.OutputDiff {
		e.Tool.OutputDiff = diff.IsUnifiedDiff(e.Tool.Output)
	}
	s.inner.Emit(e)
}
