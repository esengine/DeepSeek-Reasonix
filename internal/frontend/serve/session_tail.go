package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/state/sessionstore"
)

// sessionTail copies the newest part of a session too large to open into a new
// one and answers with its path; the source file is only read.
func (s *Server) sessionTail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		missingField(w, "path")
		return
	}
	realPath, status, err := s.sessionFileInDir(body.Path)
	if err != nil {
		writeErr(w, status, err)
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	path, dropped, err := s.ctl().ForkSessionTail(realPath)
	switch {
	case errors.Is(err, sessionstore.ErrSessionReplayLimitExceeded):
		refuse(w, http.StatusRequestEntityTooLarge, codeSessionReplayLimit, "session cannot be read even in part", nil)
	case err != nil:
		refuse(w, http.StatusInternalServerError, "session.tail_failed", err.Error(), nil)
	default:
		writeJSON(w, struct {
			Path    string `json:"path"`
			Dropped int    `json:"dropped"`
		}{Path: path, Dropped: dropped})
	}
}
