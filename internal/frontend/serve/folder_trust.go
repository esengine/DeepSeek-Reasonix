package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// workspaceTrust records the person's answer to "trust this folder?" for the
// session's workspace. The kernel moves a session still on its default posture;
// the refreshed posture reaches the page through /status.
func (s *Server) workspaceTrust(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Trust string `json:"trust"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	trust := config.WorkspaceTrust(body.Trust)
	if trust != config.WorkspaceTrusted && trust != config.WorkspaceTrustDeclined {
		badValue(w, "trust", string(config.WorkspaceTrusted), string(config.WorkspaceTrustDeclined))
		return
	}
	if err := s.ctl().DecideWorkspaceTrust(trust); err != nil {
		if errors.Is(err, control.ErrUntrustableFolder) {
			refuse(w, http.StatusConflict, "workspace.untrustable", err.Error(), map[string]any{"root": s.ctl().WorkspaceRoot()})
			return
		}
		saveFailed(w, http.StatusInternalServerError, "workspace.trust_save_failed", err)
		return
	}
	writeJSON(w, s.ctl().Posture())
}
