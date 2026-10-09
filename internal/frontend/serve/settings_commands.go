package serve

import (
	"net/http"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/event"
)

// interceptSlash answers the typed verbs that rebuild the runtime, which only
// the server can do: /model <ref>, /effort <level> and /reload. The controller's
// Submit path only lists models and reports effort.
func (s *Server) interceptSlash(w http.ResponseWriter, r *http.Request, trimmed string) bool {
	var err error
	switch {
	case strings.HasPrefix(trimmed, "/model ") && strings.TrimSpace(strings.TrimPrefix(trimmed, "/model")) != "":
		err = s.switchModelRequested(r.Context(), strings.TrimSpace(strings.TrimPrefix(trimmed, "/model")))
	case strings.HasPrefix(trimmed, "/effort ") && strings.TrimSpace(strings.TrimPrefix(trimmed, "/effort")) != "":
		err = s.switchEffort(r.Context(), strings.TrimSpace(strings.TrimPrefix(trimmed, "/effort")))
	case trimmed == "/reload":
		if err = s.reloadExtensions(r.Context()); err != nil {
			writeErr(w, http.StatusConflict, err)
			return true
		}
		s.bc.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: i18n.M.RuntimeReloaded})
	default:
		return false
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return true
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}
