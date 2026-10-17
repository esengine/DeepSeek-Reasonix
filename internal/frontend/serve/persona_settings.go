// persona_settings.go — the user's system persona as HTTP. Saving writes the
// user config and rebuilds nothing: the prompt is the provider-cached prefix,
// so the running session keeps it and new sessions get the saved persona.
package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/contract/config"
)

func (s *Server) registerPersonaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /persona", s.personaSettings)
	mux.HandleFunc("POST /persona", s.savePersonaSettings)
}

func (s *Server) personaSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().PersonaSettings())
}

func (s *Server) savePersonaSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prefix   string `json:"prefix"`
		Suffix   string `json:"suffix"`
		Complete bool   `json:"complete"`
		// A pointer so a client that omits the field keeps the default (enabled)
		// instead of saving an explicit false it never showed the user.
		IncludeRuntimeContext *bool `json:"include_runtime_context"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	p := config.PersonaConfig{
		Prefix:                body.Prefix,
		Suffix:                body.Suffix,
		Complete:              body.Complete,
		IncludeRuntimeContext: body.IncludeRuntimeContext,
	}
	if err := s.ctl().SavePersonaSettings(p); err != nil {
		switch {
		case errors.Is(err, config.ErrPersonaCompleteRequiresPrefix):
			saveFailed(w, http.StatusBadRequest, "persona.complete_requires_prefix", err)
		case errors.Is(err, config.ErrPersonaTooLarge):
			saveFailed(w, http.StatusBadRequest, "persona.too_large", err)
		default:
			// A write that failed for none of the persona reasons is the generic
			// internal class, which the frontend already words; a persona-specific
			// code would need a translation this change does not own.
			saveFailed(w, http.StatusInternalServerError, "internal.failed", err)
		}
		return
	}
	writeJSON(w, s.ctl().PersonaSettings())
}
