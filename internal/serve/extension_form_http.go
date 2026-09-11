package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type extensionFormSubmitter interface {
	SubmitExtensionForm(ctx context.Context, pluginID, surfaceID string, values map[string]any) error
}

func (s *Server) submitExtensionForm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PluginID  string         `json:"pluginId"`
		SurfaceID string         `json:"surfaceId"`
		Values    map[string]any `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.PluginID) == "" || strings.TrimSpace(body.SurfaceID) == "" {
		http.Error(w, "missing extension form identity", http.StatusBadRequest)
		return
	}
	submitter, ok := s.ctl().(extensionFormSubmitter)
	if !ok {
		http.Error(w, "extension form submission is unavailable", http.StatusConflict)
		return
	}
	if err := submitter.SubmitExtensionForm(r.Context(), body.PluginID, body.SurfaceID, body.Values); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
