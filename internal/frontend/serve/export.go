package serve

import (
	"errors"
	"net/http"
)

// conversationExporter is the controller's export capability.
type conversationExporter interface {
	ExportConversation() (path string, messages int, err error)
}

func (s *Server) registerExportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /sessions/export", s.exportConversation)
}

// exportConversation saves the conversation in the workspace the kernel runs
// in and answers where; messages 0 means there was nothing to save.
func (s *Server) exportConversation(w http.ResponseWriter, _ *http.Request) {
	ex, ok := s.ctl().(conversationExporter)
	if !ok {
		writeErr(w, http.StatusNotImplemented, errors.New("this kernel cannot export"))
		return
	}
	path, n, err := ex.ExportConversation()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"path": path, "messages": n})
}
