package serve

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func (h *Hub) forkRuntime(w http.ResponseWriter, r *http.Request) {
	rt := h.Get(r.PathValue("id"))
	if rt == nil {
		notFound(w, "runtime", r.PathValue("id"))
		return
	}
	if !rt.Local() {
		refuse(w, http.StatusBadRequest, "fork.local_only", "fork is only available for local conversations", nil)
		return
	}
	var req struct {
		SessionPath string `json:"sessionPath"`
		Turn        int    `json:"turn"`
		MsgIndex    int    `json:"msgIndex"`
		Stamp       string `json:"stamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	rt.Server.bindMu.Lock()
	defer rt.Server.bindMu.Unlock()
	ctrl := rt.Server.Controller()
	if controllerHasActiveRuntimeWork(ctrl) {
		busy(w, "fork.busy", "wait for the running turn to finish before forking", nil)
		return
	}
	path, err := ctrl.ForkTurn(req.SessionPath, req.Turn, req.MsgIndex, req.Stamp)
	if err != nil {
		switch {
		case errors.Is(err, control.ErrForkBusy):
			busy(w, "fork.busy", "wait for the running turn to finish before forking", nil)
		case errors.Is(err, control.ErrForkBoundary):
			refuse(w, http.StatusConflict, "fork.stale", "the selected conversation checkpoint is no longer valid; refresh and select the final reply again", nil)
		default:
			slog.Warn("serve: fork creation failed", "runtime", rt.ID, "code", "fork.failed")
			refuse(w, http.StatusInternalServerError, "fork.failed", "could not create the conversation fork", nil)
		}
		return
	}
	settings, ok, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !ok {
		slog.Warn("serve: fork settings unavailable", "runtime", rt.ID, "code", "fork.open_failed")
		refuse(w, http.StatusInternalServerError, "fork.open_failed", "the fork was created, but its settings cannot be read; please try again", nil)
		return
	}
	child, err := h.openWithSettings(r.Context(), OpenRequest{Root: rt.Root, SessionPath: path}, &settings)
	if err != nil {
		slog.Warn("serve: fork opening failed", "runtime", rt.ID, "code", "fork.open_failed")
		refuse(w, http.StatusInternalServerError, "fork.open_failed", "the fork was created, but cannot be opened right now; please try again", nil)
		return
	}
	writeJSON(w, child.view())
}
