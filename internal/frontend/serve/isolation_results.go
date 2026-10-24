package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
)

func (s *Server) registerIsolationResultRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /isolated-results", s.isolatedResults)
	mux.HandleFunc("POST /isolated-results/discard-preview", s.previewIsolatedDiscard)
	mux.HandleFunc("POST /isolated-results/discard", s.discardIsolated)
	mux.HandleFunc("POST /isolated-results/recover", s.recoverIsolatedIntegration)
	mux.HandleFunc("POST /isolated-results/command", s.isolationCommand)
	mux.HandleFunc("POST /isolated-results/root", s.manageRootScene)
}

func (s *Server) manageRootScene(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var request isolation.RootSceneRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&request); err != nil {
		badBody(w)
		return
	}
	owner, ok := s.ctl().(control.RootSceneAPI)
	if !ok {
		writeIsolationError(w, tool.Refusal{Code: isolation.CodeUnavailable, Message: "root scene management is unavailable"})
		return
	}
	if err := owner.ManageRootScene(r.Context(), request); err != nil {
		writeIsolationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) isolatedResults(w http.ResponseWriter, r *http.Request) {
	result, err := s.ctl().IsolationResults(r.Context())
	if err != nil {
		writeIsolationError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) previewIsolatedDiscard(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	preview, err := s.ctl().PreviewIsolatedDiscard(r.Context(), body.ID)
	if err != nil {
		writeIsolationError(w, err)
		return
	}
	writeJSON(w, preview)
}

func (s *Server) discardIsolated(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var body control.IsolatedDiscardRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().DiscardIsolated(r.Context(), body); err != nil {
		writeIsolationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recoverIsolatedIntegration(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var target isolation.RecoveryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&target); err != nil {
		badBody(w)
		return
	}
	if err := s.recoverIsolatedTarget(r.Context(), target); err != nil {
		writeIsolationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) recoverIsolatedTarget(ctx context.Context, target isolation.RecoveryRequest) error {
	s.bindMu.Lock()
	ctrl := s.ctl()
	var admissionErr error
	if current, ok := ctrl.(*control.Controller); ok {
		admissionErr = current.PrepareIsolatedRecovery(ctx, target)
	}
	s.bindMu.Unlock()
	if admissionErr != nil {
		return admissionErr
	}
	return ctrl.RecoverIsolatedIntegration(ctx, target)
}

func writeIsolationError(w http.ResponseWriter, err error) {
	if refuseMutation(w, err) {
		return
	}
	var refusal tool.Refusal
	if errors.As(err, &refusal) {
		refuse(w, isolationRefusalStatus(refusal.Code), refusal.Code, refusal.Message, nil)
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

func isolationRefusalStatus(code string) int {
	switch code {
	case isolation.CodeUnknownID:
		return http.StatusNotFound
	case isolation.CodeUnavailable, isolation.CodeNotGit, isolation.CodeUnsupportedState:
		return http.StatusNotImplemented
	default:
		return http.StatusConflict
	}
}
