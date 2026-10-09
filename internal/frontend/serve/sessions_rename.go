package serve

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"reasonix/internal/state/sessionstore"
)

// renameSessionAt sets the title of the saved conversation GET /sessions lists
// under id. The path is the listing's own, so the request names a session but
// never a place on disk; the title lives in the sidecar, safe on an open session.
func (s *Server) renameSessionAt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		missingField(w, "id")
		return
	}
	dir := s.ctl().SessionDir()
	if dir == "" {
		refuse(w, http.StatusBadRequest, "session.disabled", "sessions disabled", nil)
		return
	}
	listed, err := sessionstore.ListSessions(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, si := range listed {
		if strings.TrimSuffix(filepath.Base(si.Path), ".jsonl") != body.ID {
			continue
		}
		if err := sessionstore.RenameSession(si.Path, body.Title); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	refuse(w, http.StatusNotFound, codeSessionUnknown, "no such session", nil)
}
