package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"reasonix/internal/session/control"
)

// network returns the proxy settings. The stored password never comes back out —
// only whether one exists, so the editor can offer to keep or clear it.
func (s *Server) network(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.ctl().NetworkSettings())
}

// saveNetwork persists the proxy settings after the kernel validates them.
func (s *Server) saveNetwork(w http.ResponseWriter, r *http.Request) {
	var body struct {
		control.NetworkSettings
		Password      string `json:"password"`
		ClearPassword bool   `json:"clearPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SaveNetworkSettings(body.NetworkSettings, body.Password, body.ClearPassword); err != nil {
		saveFailed(w, http.StatusBadRequest, "network.rejected", err)
		return
	}
	writeJSON(w, s.ctl().NetworkSettings())
}

// diagnoseNetwork walks the path to the active provider. It has its own deadline
// because the failure being diagnosed is often a hang, and a diagnosis that
// hangs too is no use.
func (s *Server) diagnoseNetwork(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	writeJSON(w, map[string]any{"probes": s.ctl().DiagnoseNetwork(ctx)})
}
