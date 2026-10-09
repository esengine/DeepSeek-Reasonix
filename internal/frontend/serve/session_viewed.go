package serve

import "net/http"

func (s *Server) registerSessionMarkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /sessions/rename", s.renameSessionAt)
	mux.HandleFunc("POST /sessions/viewed", s.sessionViewed)
}

// sessionViewed clears the unread mark on the conversation this pane has open.
func (s *Server) sessionViewed(w http.ResponseWriter, _ *http.Request) {
	if err := s.ctl().MarkSessionViewed(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
