package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func (s *Server) registerProgressWatchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /progress-watch", s.progressWatchSettings)
	mux.HandleFunc("POST /progress-watch", s.saveProgressWatchSettings)
}

func (s *Server) progressWatchSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().ProgressWatchSettings())
}

// saveProgressWatchSettings needs no rebuild: the controller hands the new
// watch to the running executor, which reads it at the next round boundary.
func (s *Server) saveProgressWatchSettings(w http.ResponseWriter, r *http.Request) {
	var body control.ProgressWatchSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SaveProgressWatchSettings(body); err != nil {
		if errors.Is(err, config.ErrProgressWatchOutOfRange) {
			saveFailed(w, http.StatusBadRequest, "progress_watch.out_of_range", err)
			return
		}
		saveFailed(w, http.StatusInternalServerError, "progress_watch.save_failed", err)
		return
	}
	writeJSON(w, s.ctl().ProgressWatchSettings())
}
