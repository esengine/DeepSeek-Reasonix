// shell.go — which interpreter the shell tool runs commands under.
package serve

import (
	"encoding/json"
	"net/http"
)

func (s *Server) registerShellRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /shell", s.shellSettings)
	mux.HandleFunc("POST /shell", s.saveShellSettings)
}

func (s *Server) shellSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().ShellSettings())
}

// saveShellSettings persists the choice and rebuilds, because boot resolves the
// interpreter while assembling the runtime and binds it into the shell tool. It
// shares the provider-edit grant with the other routes that write the config of
// the machine running the kernel — a networked client must not pick which
// program the agent's commands are handed to.
func (s *Server) saveShellSettings(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "shell.editing_disabled", "shell editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Prefer string `json:"prefer"`
		Path   string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SaveShellSettings(body.Prefer, body.Path); err != nil {
		saveFailed(w, http.StatusBadRequest, "shell.rejected", err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, s.ctl().ShellSettings())
}
