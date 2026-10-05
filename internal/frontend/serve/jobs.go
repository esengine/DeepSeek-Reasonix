package serve

import "net/http"

func (s *Server) registerJobRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /jobs/{id}/cancel", s.cancelJob)
}

// cancelJob stops one of this session's background jobs. A job that already
// ended, or belongs to another session, is refused rather than acknowledged:
// the panel is showing a row that no longer answers to it.
func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	if !s.ctl().CancelJob(r.PathValue("id")) {
		refuse(w, http.StatusConflict, "job.not_running", "no running background job with this id in this session", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
