package serve

import "net/http"

// browserTabs lists the tabs this pane's agent has open, with the target id a
// window draws each one's view by.
func (s *Server) browserTabs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().BrowserTabs())
}
