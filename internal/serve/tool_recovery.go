package serve

import (
	"context"
	"encoding/json"
	"net/http"

	"reasonix/internal/control"
)

func (s *Server) registerRuntimeRecoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /tool-recovery", s.toolRecovery)
	mux.HandleFunc("POST /tool-recovery", s.foregroundMutation(s.resolveToolRecovery))
}

type toolRecoveryController interface {
	ToolRecoverySnapshot() control.ToolRecoverySnapshot
	ResolveToolRecovery(context.Context, control.ToolRecoveryRequest) (control.ToolRecoverySnapshot, error)
}

func (s *Server) toolRecovery(w http.ResponseWriter, r *http.Request) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if !s.validateExpectedSessionLocked(w, r) {
		return
	}
	ctrl, ok := s.ctl().(toolRecoveryController)
	if !ok {
		http.Error(w, "tool recovery unavailable", http.StatusNotImplemented)
		return
	}
	writeJSON(w, ctrl.ToolRecoverySnapshot())
}

func (s *Server) resolveToolRecovery(w http.ResponseWriter, r *http.Request) {
	ctrl, ok := s.ctl().(toolRecoveryController)
	if !ok {
		http.Error(w, "tool recovery unavailable", http.StatusNotImplemented)
		return
	}
	var req control.ToolRecoveryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid tool recovery request", http.StatusBadRequest)
		return
	}
	view, err := ctrl.ResolveToolRecovery(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, view)
}
