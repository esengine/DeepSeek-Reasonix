package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"reasonix/internal/contract/provider"
)

func (s *Server) fleetRecoveryPlans(w http.ResponseWriter, r *http.Request) {
	if !hostReach(r) {
		refuse(w, http.StatusNotFound, codeDeviceHostOnly, "a paired device cannot use this", nil)
		return
	}
	plans, err := s.ctl().FleetRecoveryPlans(r.PathValue("ref"))
	if err != nil {
		refuseSubagent(w, err)
		return
	}
	for i := range plans {
		for j := range plans[i].Tasks {
			task := &plans[i].Tasks[j]
			m := provider.RedactMessages([]provider.Message{{Role: provider.RoleUser, Content: task.Prompt}, {Role: provider.RoleUser, Content: task.Description}})
			task.Prompt, task.Description = m[0].Content, m[1].Content
		}
	}
	writeJSON(w, plans)
}

func (s *Server) fleetRecoveryRequest(w http.ResponseWriter, r *http.Request) {
	if !hostReach(r) {
		refuse(w, http.StatusNotFound, codeDeviceHostOnly, "a paired device cannot use this", nil)
		return
	}
	var body struct {
		ID       string `json:"id"`
		Snapshot string `json:"snapshot"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF || body.ID == "" || body.Snapshot == "" {
		badBody(w)
		return
	}
	if err := s.ctl().RequestFleetRecovery(r.PathValue("ref"), body.ID, body.Snapshot); err != nil {
		refuse(w, http.StatusConflict, "subagent.recovery_refused", err.Error(), map[string]any{"detail": err.Error()})
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
