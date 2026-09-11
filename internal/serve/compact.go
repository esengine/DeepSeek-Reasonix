package serve

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

func (s *Server) compact(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Instructions string `json:"instructions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if err := s.ctl().Compact(r.Context(), strings.TrimSpace(body.Instructions)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Persist the compacted session to disk — ctrl.Compact() only mutates in-memory.
	if err := s.ctl().Snapshot(); err != nil {
		slog.Warn("serve: snapshot after compact", "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
