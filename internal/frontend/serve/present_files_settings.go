package serve

import (
	"encoding/json"
	"net/http"
)

func (s *Server) presentFilesSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().PresentFilesSettings())
}

func (s *Server) savePresentFilesSettings(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "present_files.editing_disabled", "file-delivery tool editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if body.Enabled == nil {
		refuse(w, http.StatusBadRequest, "present_files.no_enabled", "enabled is required", nil)
		return
	}
	if err := s.ctl().SavePresentFilesSettings(*body.Enabled); err != nil {
		saveFailed(w, http.StatusInternalServerError, "present_files.save_failed", err)
		return
	}
	writeJSON(w, s.ctl().PresentFilesSettings())
}
