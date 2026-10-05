// compaction_settings.go — the economic fold boundary as HTTP: what it is set
// to, and what the session is actually running under.
package serve

import (
	"encoding/json"
	"net/http"
)

func (s *Server) registerCompactionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /compaction", s.compactionSettings)
	mux.HandleFunc("POST /compaction", s.saveCompactionSettings)
}

func (s *Server) compactionSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().CompactionSettings())
}

// saveCompactionSettings writes the economic boundary and rebuilds, because
// boot binds the bounds into every agent it assembles: without the rebuild the
// running session keeps folding where it was built to.
func (s *Server) saveCompactionSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		// A pointer, because zero is an answer here — it restores the default —
		// and a caller that cannot send it cannot undo a custom value.
		SoftLimitTokens *int `json:"soft_limit_tokens"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if body.SoftLimitTokens == nil {
		refuse(w, http.StatusBadRequest, "compaction.no_soft_limit", "soft_limit_tokens is required", nil)
		return
	}
	if err := s.ctl().SaveCompactionSettings(*body.SoftLimitTokens); err != nil {
		saveFailed(w, http.StatusBadRequest, "compaction.rejected", err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, s.ctl().CompactionSettings())
}
