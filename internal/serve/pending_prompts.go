package serve

import (
	"net/http"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

// pendingPrompts returns the controller's current approval/ask frames directly.
// Unlike the bounded SSE broadcaster, this recovery read cannot drop a frame
// when a slow remote subscriber has already fallen behind.
func (s *Server) pendingPrompts(w http.ResponseWriter, _ *http.Request) {
	prompts := make([]eventwire.Event, 0, 1)
	s.ctl().ReplayPendingPromptsTo(event.FuncSink(func(e event.Event) {
		prompts = append(prompts, eventwire.ToWire(e))
	}))
	writeJSON(w, prompts)
}
