package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (h *Hub) registerRemoteSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /remotes/{host}/sessions/remove", h.removeRemoteSession)
	mux.HandleFunc("POST /remotes/{host}/sessions/pin", h.remoteSessionFlag("pin"))
	mux.HandleFunc("POST /remotes/{host}/sessions/archive", h.remoteSessionFlag("archive"))
}

func (h *Hub) remoteSessionFlag(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.opts.Remote == nil {
			refuseNoRemote(w)
			return
		}
		var body struct {
			Path     string `json:"path"`
			Pinned   bool   `json:"pinned"`
			Archived bool   `json:"archived"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badBody(w)
			return
		}
		path := strings.TrimSpace(body.Path)
		if path == "" {
			missingField(w, "path")
			return
		}
		ep, release, err := h.remoteLink(r, r.PathValue("host"))
		if errors.Is(err, errRemoteDisabled) {
			refuse(w, http.StatusConflict, "remote.disabled", "this machine is turned off in the host book", nil)
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		defer release()
		payload := map[string]any{"path": path}
		if action == "pin" {
			payload["pinned"] = body.Pinned
		} else {
			payload["archived"] = body.Archived
		}
		if err := farRequest(r.Context(), ep, http.MethodPost, "/tree/sessions/"+action, payload, nil); err != nil {
			writeErr(w, http.StatusBadGateway, relayFarReason(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
