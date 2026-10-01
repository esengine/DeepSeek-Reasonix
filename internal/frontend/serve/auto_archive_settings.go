package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// pinsSynced records that some window has told this process which
// conversations the user pinned; until then nothing is archived automatically,
// because the kernel cannot tell what the user wanted kept.
var pinsSynced atomic.Bool

// sweepInactiveSessions is the only caller of the controller's sweep.
func (s *Server) sweepInactiveSessions() int {
	if !pinsSynced.Load() {
		return 0
	}
	return s.ctl().ArchiveInactiveSessions()
}

func (s *Server) registerAutoArchiveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auto-archive", s.autoArchiveSettings)
	mux.HandleFunc("POST /auto-archive", s.saveAutoArchiveSettings)
}

func (s *Server) autoArchiveSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().AutoArchiveSettings())
}

// saveAutoArchiveSettings needs no rebuild: the sweep reads the file on each
// pass. Turning the setting on also runs one pass now, off the request.
func (s *Server) saveAutoArchiveSettings(w http.ResponseWriter, r *http.Request) {
	var body control.AutoArchiveSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SaveAutoArchiveSettings(body); err != nil {
		if errors.Is(err, config.ErrAutoArchiveOutOfRange) {
			saveFailed(w, http.StatusBadRequest, "auto_archive.out_of_range", err)
			return
		}
		saveFailed(w, http.StatusInternalServerError, "auto_archive.save_failed", err)
		return
	}
	if body.Enabled {
		go s.sweepInactiveSessions()
	}
	writeJSON(w, s.ctl().AutoArchiveSettings())
}
