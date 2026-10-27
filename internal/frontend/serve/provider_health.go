package serve

import (
	"errors"
	"net/http"

	"reasonix/internal/session/control"
)

func (s *Server) providerHealthSettings(w http.ResponseWriter, _ *http.Request) {
	got, err := s.ctl().ProviderHealthSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, got)
}

func (s *Server) saveProviderHealthSettings(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body control.ProviderHealthSettings
	if !decodeProviderBody(w, r, &body) {
		return
	}
	if err := s.ctl().SaveProviderHealthSettings(body.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.providerHealthSettings(w, r)
}

func (s *Server) providerHealth(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeProviderBody(w, r, &body) {
		return
	}
	if s.resolver != nil {
		writeJSON(w, control.ProviderHealthReading{Connection: "unknown", Wallet: "unknown"})
		return
	}
	got, err := s.ctl().ProviderHealth(r.Context(), body.Name)
	if errors.Is(err, control.ErrProviderHealthNotFound) {
		notFound(w, "provider", body.Name)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, got)
}
