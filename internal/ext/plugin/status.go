// Where connection-state changes are announced. Kept out of plugin.go because a
// status change has no caller to answer: a lazy server connects in the
// background, long after whoever configured it went away.
package plugin

import (
	"fmt"

	"reasonix/internal/contract/event"
)

// SetStatusSink installs where connection-state changes are announced. The Host
// holds the sink rather than taking it per call, since the announcement happens
// with nobody waiting on it. Safe before or after servers connect; a nil sink is
// tolerated and simply drops the announcements.
func (h *Host) SetStatusSink(sink event.Sink) {
	h.statusMu.Lock()
	h.statusSink = sink
	h.statusMu.Unlock()
}

// announce reports a change in what /mcp would answer. Never called while
// holding h.mu: a sink writes to a frontend and must not run under the lock a
// status read would need.
func (h *Host) announce(format string, args ...any) {
	h.statusMu.RLock()
	sink := h.statusSink
	h.statusMu.RUnlock()
	if sink == nil {
		return
	}
	sink.Emit(event.Event{Kind: event.MCPSurfaceReady, Text: fmt.Sprintf(format, args...)})
}
