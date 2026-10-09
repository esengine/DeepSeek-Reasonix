package serve

import (
	"encoding/json"
	"errors"
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

// The notes route tells three kinds of failure apart: a version that is not a
// release is the caller's, a release with no notes is the catalog's, and a
// mirror that cannot be read or answers nonsense is a dependency's.
const (
	codeNotesBadVersion  = "studio.notes_bad_version"
	codeNotesAbsent      = "studio.notes_absent"
	codeNotesUnreachable = "studio.notes_unreachable"
	codeNotesTooLarge    = "studio.notes_too_large"
)

// The routes are registered whether or not a shell declared an install, so
// "this kernel is not a Studio" is an answer with a code on it rather than a
// path that quietly falls through to the hub's default runtime.
func (h *Hub) registerStudioVersionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /studio/versions", h.readStudioVersions)
	mux.HandleFunc("POST /studio/pin", h.pinStudioVersion)
	mux.HandleFunc("GET /studio/versions/{version}/notes", h.readStudioNotes)
}

// notesReader is a variable so a test answers from a fake mirror; production
// reads through update.ReadNotes and its fixed address.
var notesReader = update.ReadNotes

func (h *Hub) readStudioNotes(w http.ResponseWriter, r *http.Request) {
	if h.opts.Install == nil {
		refuse(w, http.StatusNotFound, codeNoInstall, "no Studio install was declared for this kernel", nil)
		return
	}
	read := h.notes
	if read == nil {
		read = notesReader
	}
	notes, err := read(r.Context(), *h.opts.Install, r.PathValue("version"), r.URL.Query().Get("retry") == "1")
	switch {
	case err == nil:
		writeJSON(w, notes)
	case errors.Is(err, update.ErrNotesBadVersion):
		refuse(w, http.StatusBadRequest, codeNotesBadVersion, err.Error(), nil)
	case errors.Is(err, update.ErrNotesAbsent):
		refuse(w, http.StatusNotFound, codeNotesAbsent, err.Error(), nil)
	case errors.Is(err, update.ErrNotesTooLarge):
		refuse(w, http.StatusBadGateway, codeNotesTooLarge, err.Error(), nil)
	case errors.Is(err, update.ErrNotesUnreachable):
		refuse(w, http.StatusBadGateway, codeNotesUnreachable, err.Error(), nil)
	default:
		// The caller going away is not the mirror failing.
		w.WriteHeader(http.StatusRequestTimeout)
	}
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
