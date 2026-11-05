package serve

import "net/http"

func (s *Server) registerChangedSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings-changed", s.changedSettings)
}

func (s *Server) changedSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().ChangedSettings())
}
