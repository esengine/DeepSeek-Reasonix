package serve

import (
	"sync"

	"reasonix/internal/contract/event"
)

// skillNotices is what a host has already told the person about skills on
// disk. Discovery runs for every window the host opens and reports the same
// missing description each time; the problem is the file's, so one telling is
// enough for as long as the host runs.
type skillNotices struct{ said sync.Map }

// once passes a diagnostic into sink unless this host has already said exactly
// that text into any window.
func (n *skillNotices) once(sink event.Sink) event.Sink {
	return event.FuncSink(func(e event.Event) {
		if _, dup := n.said.LoadOrStore(e.Text, struct{}{}); dup {
			return
		}
		sink.Emit(e)
	})
}

// diagnosticSink is where a build of this server's controller reports skill
// discovery warnings: through its hub's shared record when a hub claimed it.
func (s *Server) diagnosticSink() event.Sink {
	sink := s.rebuildSink()
	if s.skillNotices == nil {
		return sink
	}
	return s.skillNotices.once(sink)
}
