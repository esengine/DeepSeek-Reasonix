package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"reasonix/internal/contract/tool"

	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
)

type hostIsolatedResults struct {
	control.IsolationResultsAPI
	server *Server
}

func (h hostIsolatedResults) RecoverIsolatedIntegration(ctx context.Context, target isolation.RecoveryRequest) error {
	return h.server.recoverIsolatedTarget(ctx, target)
}

func (h hostIsolatedResults) ManageRootScene(ctx context.Context, request isolation.RootSceneRequest) error {
	if owner, ok := h.IsolationResultsAPI.(control.RootSceneAPI); ok {
		return owner.ManageRootScene(ctx, request)
	}
	return tool.Refusal{Code: isolation.CodeUnavailable, Message: "root scene management is unavailable"}
}

func (s *Server) isolationCommand(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var body struct {
		Args string `json:"args"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 96<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	text, err := control.IsolationCommandText(r.Context(), hostIsolatedResults{IsolationResultsAPI: s.ctl(), server: s}, body.Args)
	if err != nil {
		writeIsolationError(w, err)
		return
	}
	writeJSON(w, struct {
		Text string `json:"text"`
	}{text})
}
