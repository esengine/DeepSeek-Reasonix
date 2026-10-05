package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (h *Hub) moveWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path      string `json:"path"`
		Direction int    `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	dir := strings.TrimSpace(body.Path)
	if dir == "" {
		missingField(w, "path")
		return
	}
	if body.Direction != -1 && body.Direction != 1 {
		badValue(w, "direction", "-1", "1")
		return
	}
	err := moveWorkspace(r.Context(), dir, body.Direction)
	if errors.Is(err, errWorkspaceNotRemembered) {
		notFound(w, "workspace", dir)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
