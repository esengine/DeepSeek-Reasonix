package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/session/control"
)

const codeModelModeUnsupported = "model_mode.unsupported"

// modelMode switches the session's model mode; an empty mode turns it off. It
// is session state on the controller, never written to config, and a model
// that does not declare the mode is refused rather than sent a request it 400s.
func (s *Server) modelMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode *string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if body.Mode == nil {
		missingField(w, "mode")
		return
	}
	mode := strings.TrimSpace(*body.Mode)
	if err := s.ctl().SetModelMode(mode); err != nil {
		if errors.Is(err, control.ErrModelModeUnsupported) {
			writeErr(w, http.StatusBadRequest, refusal(http.StatusBadRequest, codeModelModeUnsupported, err,
				map[string]any{"mode": mode, "model": currentModelRef(s.ctl())}))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
