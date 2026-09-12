package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"reasonix/internal/control"
)

// composerProfile applies every controller-facing composer axis in one request.
// bindMu serializes it with submit and controller/session replacement, while the
// controller commits durable Goal state before the infallible runtime axes.
func (s *Server) composerProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CollaborationMode          string  `json:"collaborationMode"`
		ToolApprovalMode           string  `json:"toolApprovalMode"`
		Goal                       string  `json:"goal"`
		ExpectedPermissionRevision *uint64 `json:"expectedPermissionRevision,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	collaborationMode := strings.ToLower(strings.TrimSpace(body.CollaborationMode))
	switch collaborationMode {
	case "normal", "plan", "goal":
	default:
		http.Error(w, "collaboration mode must be normal, plan, or goal", http.StatusBadRequest)
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if !s.validateExpectedSessionLocked(w, r) {
		return
	}
	var drained []string
	var err error
	if body.ExpectedPermissionRevision != nil {
		ctrl, ok := s.ctl().(*control.Controller)
		if !ok {
			http.Error(w, "revision-checked permission profiles are unavailable", http.StatusNotImplemented)
			return
		}
		drained, err = ctrl.ApplyComposerProfileAt(collaborationMode == "plan", body.ToolApprovalMode, body.Goal, *body.ExpectedPermissionRevision)
	} else {
		drained, err = s.ctl().ApplyComposerProfile(collaborationMode == "plan", body.ToolApprovalMode, body.Goal)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"drainedApprovalIDs": drained})
}
