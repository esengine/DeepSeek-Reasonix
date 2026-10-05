// welcome.go — whether the opening sequence has been seen on this machine.
package serve

import (
	"net/http"

	"reasonix/internal/contract/config"
)

func (s *Server) registerWelcomeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /welcome", s.welcomeState)
	mux.HandleFunc("POST /welcome", s.markWelcomed)
}

// welcomeState reports whether the opening sequence still owes the user a
// showing. A read failure answers "seen": a config the host cannot read is no
// reason to replay an introduction, and the sequence must never be the thing
// standing between someone and their session.
func (s *Server) welcomeState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	seen := true
	if cfg, err := config.Load(); err == nil {
		seen = cfg.Desktop.Welcomed
	}
	writeJSON(w, struct {
		Seen bool `json:"seen"`
	}{Seen: seen})
}

// markWelcomed records the sequence as played. The frontend calls it when the
// sequence ends or the user skips it — both are "this person has now met the
// app", so both close it out.
func (s *Server) markWelcomed(w http.ResponseWriter, _ *http.Request) {
	edit := config.LoadForEdit(config.UserConfigPath())
	edit.Desktop.Welcomed = true
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
