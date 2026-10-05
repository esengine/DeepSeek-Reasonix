package serve

import (
	"encoding/json"
	"net/http"

	"reasonix/internal/session/control"
)

// maxDroppedPaths bounds one drop. The body limit alone would not: each path
// costs a symlink resolution and a stat, and a folder emptied onto the window
// is a plausible accident.
const maxDroppedPaths = 64

// droppedRef answers one path with the token to put in the line, or with why
// that path has none. Per-path rather than per-request: dropping six files of
// which one has since been moved should attach the five.
type droppedRef struct {
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
	// Image says a text-only model will not see this one. No omitempty: false is
	// the answer for every file that is not a picture, and dropping it leaves
	// "not an image" indistinguishable from "nobody said".
	Image bool   `json:"image"`
	Error string `json:"error,omitempty"`
}

// drop names what a dropped path is called inside a turn. The host has to say:
// whether a path is inside the workspace compares two spellings of a location,
// and the reference minted for anything outside must survive an @ grammar the
// page cannot see. A browser tab never reaches here — it holds bytes and no
// path, which is what POST /attachments is for.
func (s *Server) drop(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if len(body.Paths) > maxDroppedPaths {
		refuse(w, http.StatusBadRequest, "drop.too_many_paths", "too many paths in one drop", map[string]any{"count": len(body.Paths), "limit": maxDroppedPaths})
		return
	}
	ctl := s.ctl()
	out := make([]droppedRef, 0, len(body.Paths))
	for _, path := range body.Paths {
		token, display, err := ctl.DroppedRef(path)
		if err != nil {
			out = append(out, droppedRef{Error: err.Error()})
			continue
		}
		out = append(out, droppedRef{Ref: "@" + token, Path: display, Image: control.RefIsImage(display)})
	}
	writeJSON(w, out)
}
