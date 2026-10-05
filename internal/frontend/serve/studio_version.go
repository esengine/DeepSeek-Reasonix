package serve

import (
	"encoding/json"
	"net/http"

	"reasonix/internal/platform/update"
)

// codeNoInstall answers a version route on a kernel nothing declared an install
// for. It is a refusal rather than an empty hub: "no build to change" and "a
// catalog that came back empty" are different answers, and a panel that folds
// them together offers to update a server that has no application around it.
const codeNoInstall = "studio.no_install"

// codePinRejected answers a pin the config layer would not take.
const codePinRejected = "studio.pin_rejected"

// The routes are registered whether or not a shell declared an install, so
// "this kernel is not a Studio" is an answer with a code on it rather than a
// path that quietly falls through to the hub's default runtime.
func (h *Hub) registerStudioVersionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /studio/versions", h.readStudioVersions)
	mux.HandleFunc("POST /studio/pin", h.pinStudioVersion)
}

// readStudioVersions answers what is published and what that means for this
// install. The shell states which build it is; everything downstream of that —
// newer, pinned, latest, the order of the rows — is decided here, so two shells
// cannot disagree about it.
func (h *Hub) readStudioVersions(w http.ResponseWriter, r *http.Request) {
	if h.opts.Install == nil {
		refuse(w, http.StatusNotFound, codeNoInstall, "no Studio install was declared for this kernel", nil)
		return
	}
	writeJSON(w, update.Hub(r.Context(), *h.opts.Install))
}

func (h *Hub) pinStudioVersion(w http.ResponseWriter, r *http.Request) {
	if h.opts.Install == nil {
		refuse(w, http.StatusNotFound, codeNoInstall, "no Studio install was declared for this kernel", nil)
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		refuse(w, http.StatusBadRequest, codePinRejected, err.Error(), nil)
		return
	}
	// An empty version releases the hold; it is the only way back to following
	// the catalog, so it is a value rather than a missing field.
	if err := update.Pin(req.Version); err != nil {
		refuse(w, http.StatusConflict, codePinRejected, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
