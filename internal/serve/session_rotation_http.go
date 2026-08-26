package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"reasonix/internal/control"
)

func (s *Server) planDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string                     `json:"id"`
		Action   control.PlanDecisionAction `json:"action"`
		Feedback string                     `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := s.ctl().ResolvePlanDecisionWithFeedback(body.ID, body.Action, body.Feedback); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	s.ctl().SetPlanMode(body.On)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearSession(w http.ResponseWriter, _ *http.Request) {
	// Clear rotates the session path just like /new, but also removes the old
	// transcript artifacts. Keep controller mutation and lease rebinding under
	// one binding lock so remote clients never observe split ownership.
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if err := s.ctl().ClearSession(); err != nil {
		if control.IsSessionRotationBusy(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.bc.ResetSession()
	if err := s.rebindSessionLease(s.ctl().SessionPath()); err != nil {
		http.Error(w, sessionInUseError(err), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
