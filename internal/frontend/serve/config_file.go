// config_file.go — the config file itself when it is the thing that is wrong.
package serve

import (
	"net/http"
)

func (s *Server) registerConfigFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /config/problem", s.configProblem)
	mux.HandleFunc("POST /config/repair", s.repairConfigFile)
}

// configProblem answers null when the file reads, so a settings surface asks
// once instead of each panel learning it from its own refused save. It rides
// the edit grant despite only reading: it returns a line of the file as
// written, and the line that stopped the parser may be the one holding a key.
func (s *Server) configProblem(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "config.editing_disabled", "config editing is not enabled on this server", nil)
		return
	}
	writeJSON(w, s.ctl().ConfigProblem())
}

// repairConfigFile rewrites the file after copying the original beside it, then
// rebuilds so the runtime stops running on what it booted with. It shares the
// provider-edit grant with every other route that writes the config of the
// machine the kernel runs on.
func (s *Server) repairConfigFile(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "config.editing_disabled", "config editing is not enabled on this server", nil)
		return
	}
	backup, err := s.ctl().RepairConfigFile()
	if err != nil {
		refuse(w, http.StatusUnprocessableEntity, "config.not_repairable", err.Error(), map[string]any{"detail": err.Error()})
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, map[string]any{"backup": backup, "problem": s.ctl().ConfigProblem()})
}
