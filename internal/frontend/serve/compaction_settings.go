// compaction_settings.go — the compaction trigger as HTTP: what it is set to,
// and what the session is actually running under.
package serve

import "net/http"

func (s *Server) registerCompactionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /compaction", s.compactionSettings)
}

func (s *Server) compactionSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().CompactionSettings())
}
