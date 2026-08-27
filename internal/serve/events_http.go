package serve

import (
	"fmt"
	"net/http"
	"time"

	"reasonix/internal/event"
)

// Keepalives prevent quiet turns from being closed by common 30–60 s proxies.
const sseKeepaliveInterval = 15 * time.Second

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	var ch <-chan []byte
	var unsubscribe func()
	// Session switches also hold bindMu. Capture, subscribe, and replay in that
	// epoch so a promoted controller cannot broadcast its prompt before this
	// current-only subscriber exists.
	s.bindMu.Lock()
	ctrl := s.ctl()
	currentPath := ctrl.SessionPath()
	ctrl.ReplayPendingPromptsWith(func() event.Sink {
		if r.URL.Query().Get("all") == "1" {
			ch, unsubscribe = s.bc.SubscribeAll()
		} else {
			ch, unsubscribe = s.bc.Subscribe()
		}
		return event.FuncSink(func(e event.Event) {
			if currentPath != "" {
				e.SessionPath = currentPath
			}
			s.bc.EmitTo(ch, e)
		})
	})
	s.bindMu.Unlock()
	defer unsubscribe()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
