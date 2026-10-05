// extensions.go — the interactive half of an extension's UI surface.
package serve

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Publications travel out on the event stream as extension_surface, so these
// three routes only carry what the user sends back: the action list a frontend
// offers, an invocation, and a published form's values.
func (s *Server) registerExtensionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /extensions/actions", s.extensionActions)
	mux.HandleFunc("POST /extensions/action", s.invokeExtensionAction)
	mux.HandleFunc("POST /extensions/submit", s.submitExtensionForm)
}

type extensionActionEntry struct {
	PluginID string `json:"pluginId"`
	ActionID string `json:"actionId"`
	Label    string `json:"label,omitempty"`
	Slash    string `json:"slash,omitempty"`
}

// No extension runtime means no actions, which is an empty list and not an
// error: a frontend renders the same empty affordance either way.
func (s *Server) extensionActions(w http.ResponseWriter, _ *http.Request) {
	actions := s.ctl().ExtensionActions()
	out := make([]extensionActionEntry, 0, len(actions))
	for _, a := range actions {
		out = append(out, extensionActionEntry{
			PluginID: a.PluginID,
			ActionID: a.ActionID,
			Label:    a.Label,
			Slash:    a.Slash,
		})
	}
	writeJSON(w, out)
}

func (s *Server) invokeExtensionAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string            `json:"name"`
		Args map[string]string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		missingField(w, "name")
		return
	}
	// The extension owns the outcome: a declined action is its answer, not a
	// server fault, and its message is already credential-redacted by the hub.
	message, err := s.ctl().InvokeExtensionAction(r.Context(), name, req.Args)
	if err != nil {
		saveFailed(w, http.StatusUnprocessableEntity, "extension.action_failed", err)
		return
	}
	writeJSON(w, map[string]string{"message": message})
}

func (s *Server) submitExtensionForm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PluginID  string         `json:"pluginId"`
		SurfaceID string         `json:"surfaceId"`
		Values    map[string]any `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	pluginID := strings.TrimSpace(req.PluginID)
	surfaceID := strings.TrimSpace(req.SurfaceID)
	if pluginID == "" || surfaceID == "" {
		missingField(w, "pluginId and surfaceId")
		return
	}
	if err := s.ctl().SubmitExtensionForm(r.Context(), pluginID, surfaceID, req.Values); err != nil {
		saveFailed(w, http.StatusUnprocessableEntity, "extension.form_rejected", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
