// surfaces.go — where the user decided an extension's surface belongs.
package serve

import (
	"encoding/json"
	"maps"
	"net/http"
	"strings"

	"reasonix/internal/contract/config"
)

// An extension asks for a place and the frontend offers a default; this is the
// third answer, and it wins over both. It lives in config rather than in the
// browser so a window reopened tomorrow puts things back where the user left
// them, and it is keyed by surface rather than by plugin so one extension's two
// surfaces can sit in different places.
func (s *Server) registerSurfaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /surfaces", s.surfaceSlots)
	mux.HandleFunc("POST /surfaces", s.assignSurfaceSlot)
}

const maxSurfaceSlots = 64

func (s *Server) surfaceSlots(w http.ResponseWriter, r *http.Request) {
	slots := map[string]string{}
	if cfg, err := config.Load(); err == nil {
		maps.Copy(slots, cfg.Desktop.SurfaceSlots)
	}
	writeJSONCached(w, r, map[string]any{"slots": slots})
}

// assignSurfaceSlot records one placement. An empty slot is the user taking it
// back out of the chrome, which is a real choice rather than a missing one, so
// it removes the entry and lets the extension's own suggestion apply again.
func (s *Server) assignSurfaceSlot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Surface string `json:"surface"`
		Slot    string `json:"slot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	surface := strings.TrimSpace(req.Surface)
	slot := strings.TrimSpace(req.Slot)
	// The names are a frontend's own vocabulary, so this end checks only that
	// they are names: what a slot means is not knowable from here.
	if surface == "" || len(surface) > 128 || len(slot) > 64 {
		missingField(w, "surface")
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	if edit.Desktop.SurfaceSlots == nil {
		edit.Desktop.SurfaceSlots = map[string]string{}
	}
	if slot == "" {
		delete(edit.Desktop.SurfaceSlots, surface)
	} else {
		if _, known := edit.Desktop.SurfaceSlots[surface]; !known && len(edit.Desktop.SurfaceSlots) >= maxSurfaceSlots {
			refuse(w, http.StatusUnprocessableEntity, "surface.too_many_slots",
				"too many surface placements are recorded; clear one first",
				map[string]any{"limit": maxSurfaceSlots})
			return
		}
		edit.Desktop.SurfaceSlots[surface] = slot
	}
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
