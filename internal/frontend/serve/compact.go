package serve

import (
	"log/slog"
	"net/http"

	"reasonix/internal/runtime/agent"
)

func (s *Server) compact(w http.ResponseWriter, r *http.Request) {
	verdict, err := s.ctl().Compact(r.Context(), agent.CompactRequest{})
	if err != nil {
		// A declined fold is a verdict, not a fault: the candidate was no smaller
		// than what it would replace. 500 made every frontend show it as a failure.
		if agent.IsCompactionDeclined(err) {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// A request the host declined answered 204 as well, so a caller could not
	// tell a fold from a session it decided not to pay for. The reason is a
	// code the caller matches; the sentence is what it prints without one.
	if !verdict.Compacted() {
		writeJSON(w, map[string]any{
			"compacted": false,
			"reason":    string(verdict.Reason),
			"detail":    agent.CompactDeclineText(verdict.Reason),
		})
		return
	}
	// Persist the compacted session to disk — ctrl.Compact() only mutates in-memory.
	if err := s.ctl().Snapshot(); err != nil {
		slog.Warn("serve: snapshot after compact", "err", err)
	}
	writeJSON(w, map[string]any{"compacted": true})
}
