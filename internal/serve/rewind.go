package serve

import (
	"encoding/json"
	"net/http"

	"reasonix/internal/control"
)

// rewind rewinds the session to a checkpoint.
func (s *Server) rewind(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Turn  int    `json:"turn"`
		Scope string `json:"scope"` // "code", "conversation", "both"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Turn < 0 {
		http.Error(w, "missing turn", http.StatusBadRequest)
		return
	}
	scope := control.RewindBoth
	switch body.Scope {
	case "both":
	case "code":
		scope = control.RewindCode
	case "conversation":
		scope = control.RewindConversation
	default:
		http.Error(w, "scope must be code, conversation, or both", http.StatusBadRequest)
		return
	}
	// Rewind may intentionally switch to a fork. Serialize the controller and
	// lease handoff with every other session-path-changing endpoint.
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if err := s.ctl().Rewind(body.Turn, scope); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if scope != control.RewindCode {
		s.bc.ResetSession()
	}
	w.WriteHeader(http.StatusNoContent)
}
