// pane_sink.go — the sink a pane's controller emits into, across every rebuild.
package serve

import (
	"reasonix/internal/base/nilutil"
	"reasonix/internal/contract/event"
)

// SetPaneSink records the host's decoration around bc — the sink the controller
// was built to emit into. Every replacement emits into it too: a model,
// extension or workspace switch rebuilds the controller, and one wired to the
// bare broadcaster leaves the host's own observers — a status icon's fold,
// desktop notifications, usage counters — deaf for the rest of the pane's life.
func (s *Server) SetPaneSink(sink event.Sink) {
	if s == nil || nilutil.IsNil(sink) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paneSink = sink
}

// rebuildSink is what a replacement controller emits into: the host's
// decoration when it gave one, the bare broadcaster otherwise.
func (s *Server) rebuildSink() event.Sink {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if nilutil.IsNil(s.paneSink) {
		return s.bc
	}
	return s.paneSink
}
