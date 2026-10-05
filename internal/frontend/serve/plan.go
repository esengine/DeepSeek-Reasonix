package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"reasonix/internal/session/control"
)

// planDecision answers a plan card without collapsing its three outcomes into
// the approval boolean. Start, revise, and exit are different transitions, and
// the frontend sends back the id the host issued rather than describing the
// state it thinks the run is in.
func (s *Server) planDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		missingField(w, "id")
		return
	}
	err := s.ctl().ResolvePlanDecision(body.ID, control.PlanDecisionAction(strings.TrimSpace(body.Action)))
	if err != nil {
		// A decision the host no longer holds is ordinary concurrency, not a
		// client fault: the run moved on while the card was open.
		refuse(w, http.StatusConflict, "plan.decision_stale", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	s.ctl().SetPlanMode(body.On)
	w.WriteHeader(http.StatusNoContent)
}
