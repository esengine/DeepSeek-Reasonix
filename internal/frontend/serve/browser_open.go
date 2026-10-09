package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"reasonix/internal/platform/browser"
)

// browserOpen opens a page in the session's browser. The window's own tabs and
// the agent's are the same tabs: whoever opened one, the other can see it and
// act on it.
func (s *Server) browserOpen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL    string `json:"url"`
		Tab    string `json:"tab"`
		NewTab bool   `json:"newTab"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		missingField(w, "url")
		return
	}
	tab, err := s.ctl().BrowserOpen(r.Context(), body.URL, body.Tab, body.NewTab)
	if err != nil {
		params := map[string]any{"url": body.URL, "error": err.Error()}
		if tab.ID != "" {
			params["tab"] = tab.ID
		}
		switch browser.CodeOf(err) {
		case browser.CodeEngineMissing:
			refuse(w, http.StatusBadRequest, "browser.engine_missing", err.Error(), params)
		case browser.CodeEngineFailed:
			refuse(w, http.StatusBadRequest, "browser.engine_failed", err.Error(), params)
		case browser.CodeProfileBusy:
			refuse(w, http.StatusBadRequest, "browser.profile_busy", err.Error(), params)
		default:
			refuse(w, http.StatusBadRequest, "browser.open_failed", err.Error(), params)
		}
		return
	}
	writeJSON(w, tab)
}
