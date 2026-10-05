package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/platform/appupdate"
	"reasonix/internal/platform/update"
)

// UpdateHost is the desktop application this kernel runs inside, as far as the
// hub is concerned. Nil leaves the update routes unregistered rather than
// answering for an application nobody here owns: a process that cannot bring
// one to a replaceable state and start its successor has no business declaring
// a replacement healthy either, or moving it to another version.
type UpdateHost interface {
	// AcknowledgeLaunchHealth retires the update this launch booted from. It
	// takes nothing: which transaction that is was read at startup, so the
	// caller says only that the launch it is in is working.
	AcknowledgeLaunchHealth() error
	// StartInstall moves the application to a published version and returns
	// once the move is under way. Which build runs and where it lives is passed
	// in, so the hub's declared Install stays the one answer.
	StartInstall(install update.Install, target string) error
	// CommitInstall installs the release StartInstall left ready and restarts
	// into it. It is a separate act because it ends every session running here.
	CommitInstall(target string) error
	// InstallProgress is what that move is doing, as a projection.
	InstallProgress() update.Progress
}

const (
	codeUpdateRejected = "update.rejected"
	// Told apart because they are different things to do about it: wait for the
	// move already running, or fix what was asked for.
	codeInstallRunning  = "update.install_running"
	codeInstallRejected = "update.install_rejected"
	// The restart is refused while work runs here unless the person saw that
	// and asked anyway, and refused when there is nothing verified to restart into.
	codeRestartBusy    = "update.restart_busy"
	codeNothingToApply = "update.nothing_ready"
)

func (h *Hub) registerUpdateRoutes(mux *http.ServeMux) {
	if h.opts.Update == nil {
		return
	}
	mux.HandleFunc("POST /update/health", h.acknowledgeLaunchHealth)
	mux.HandleFunc("POST /update/install", h.startInstall)
	mux.HandleFunc("GET /update/install", h.readInstallProgress)
	mux.HandleFunc("POST /update/restart", h.commitInstall)
}

// The swap is performed by a process that cannot judge it. This is the other
// half: the application that booted from the replacement says it is working,
// and only then is the rollback material retired.
func (h *Hub) acknowledgeLaunchHealth(w http.ResponseWriter, r *http.Request) {
	if err := h.opts.Update.AcknowledgeLaunchHealth(); err != nil {
		refuse(w, http.StatusConflict, codeUpdateRejected, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startInstall answers as soon as the move is under way, not when it is done: an
// install that worked ends by ending this process, so a request that waited for
// it would be waiting for a reply nothing is left to send. What a client watches
// afterwards is the progress projection, and then the application coming back.
func (h *Hub) startInstall(w http.ResponseWriter, r *http.Request) {
	// Both gates, because they answer different questions. Owning the
	// application is what makes replacing it this process's business; the
	// declared install is which build is being replaced and where it lives.
	if h.opts.Install == nil {
		refuse(w, http.StatusNotFound, codeNoInstall, "no Studio install was declared for this kernel", nil)
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		refuse(w, http.StatusBadRequest, codeInstallRejected, err.Error(), nil)
		return
	}
	switch err := h.opts.Update.StartInstall(*h.opts.Install, req.Version); {
	case errors.Is(err, appupdate.ErrInstallInFlight):
		refuse(w, http.StatusConflict, codeInstallRunning, err.Error(), nil)
		return
	case err != nil:
		refuse(w, http.StatusBadRequest, codeInstallRejected, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Hub) readInstallProgress(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, h.opts.Update.InstallProgress())
}

// commitInstall is the restart the person allowed. Work running in a local pane
// or a background job ends with this process, so it is counted and refused
// unless the request says the person has already seen that. A remote pane's
// work lives on its own kernel and survives the restart.
func (h *Hub) commitInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
		Force   bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		refuse(w, http.StatusBadRequest, codeInstallRejected, err.Error(), nil)
		return
	}
	if !req.Force {
		if n := h.runningWork(); n > 0 {
			busy(w, codeRestartBusy, "work is running and would end with the restart", map[string]any{"n": n})
			return
		}
	}
	switch err := h.opts.Update.CommitInstall(req.Version); {
	case errors.Is(err, appupdate.ErrNothingReady):
		refuse(w, http.StatusConflict, codeNothingToApply, err.Error(), nil)
		return
	case err != nil:
		refuse(w, http.StatusBadRequest, codeInstallRejected, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Hub) runningWork() int {
	n := h.runningJobs()
	for _, rt := range h.localRuntimes() {
		if rt.Server != nil && rt.Server.Controller().Running() {
			n++
		}
	}
	return n
}
