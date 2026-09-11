package serve

import (
	"encoding/json"
	"net/http"
)

// mcpInteraction resolves an mcp_interaction prompt after its transition is
// durable, mirroring the ask answer endpoint. Form values ride this call only.
func (s *Server) mcpInteraction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string         `json:"id"`
		Action  string         `json:"action"`
		Content map[string]any `json:"content,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" || body.Action == "" {
		http.Error(w, "missing id or action", http.StatusBadRequest)
		return
	}
	if err := s.ctl().AnswerMCPInteractionChecked(body.ID, body.Action, body.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
