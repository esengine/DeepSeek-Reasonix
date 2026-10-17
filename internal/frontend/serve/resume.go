// Session switching over HTTP: /resume binds this runtime to another
// transcript. It lives apart from serve.go because the swap has three owners to
// keep in step — the controller's rotation gate, the session lease, and the
// broadcaster's ledger — and getting them out of order is how one conversation
// ends up written into another.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/session/control"
	"reasonix/internal/state/store"
)

// resume loads a previous session from a JSONL file.
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		missingField(w, "path")
		return
	}
	if status, err := s.resumeInto(body.Path); err != nil {
		writeErr(w, status, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resumeInto binds this runtime to the transcript at path, answering with the
// status a refusal deserves so both the HTTP handler and a hub opening a
// session in a new pane refuse for the same reasons.
func (s *Server) resumeInto(path string) (int, error) {
	dir := s.ctl().SessionDir()
	if dir == "" {
		return http.StatusBadRequest, refusal(http.StatusBadRequest, "session.disabled", errors.New("sessions disabled"), nil)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return http.StatusBadRequest, refusal(http.StatusBadRequest, codeSessionBadPath, errors.New("invalid session dir"), nil)
	}
	realDir, dirErr := filepath.EvalSymlinks(absDir)
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil || !store.IsSessionTranscriptName(filepath.Base(absPath)) {
		return http.StatusBadRequest, refusal(http.StatusBadRequest, codeSessionBadPath, errors.New("invalid session path"), nil)
	}
	// A 1.x v4 conversation the list offered becomes a transcript here first.
	if filepath.Dir(absPath) == absDir {
		if err := sessionstore.PrepareSessionPath(absPath); err != nil {
			return http.StatusBadRequest, fmt.Errorf("open 1.x session: %w", err)
		}
	}
	// A transcript that is gone lands here: the delete took the file, and the
	// list the click came from was drawn before it did.
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return pathUnresolved(err)
	}
	// A transcript that exists cannot be inside a directory that does not.
	if errors.Is(dirErr, fs.ErrNotExist) {
		return http.StatusForbidden, refusal(http.StatusForbidden, codeSessionOutside, errors.New("path outside session dir"), nil)
	}
	if dirErr != nil {
		return pathUnresolved(dirErr)
	}
	if realPath == realDir || !strings.HasPrefix(realPath, realDir+string(os.PathSeparator)) {
		return http.StatusForbidden, refusal(http.StatusForbidden, codeSessionOutside, errors.New("path outside session dir"), nil)
	}
	if sessionstore.IsCleanupPending(realPath) {
		return http.StatusBadRequest, refusal(http.StatusBadRequest, "session.pending_cleanup", errors.New("session is pending cleanup"), nil)
	}
	settings, err := branchSettings(realPath)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	// Serialize with /new, /fork, and switchModel so the controller and lease
	// cannot land on different sessions. Validate first to avoid slow holders.
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	// Snapshot the current session before switching away — while this process
	// still holds its lease.
	if err := s.ctl().Snapshot(); err != nil {
		slog.Warn("serve: snapshot before resume", "err", err)
	}
	// Take the write lease when it is free. A session another runtime writes
	// opens read-only rather than being refused: the lease stops two windows
	// writing one transcript over each other, and reading is not that.
	if s.leases != nil {
		if _, err := s.leases.Attach(realPath); err != nil {
			return http.StatusInternalServerError, fmt.Errorf("session lease: %w", err)
		}
	}
	loaded, err := sessionstore.LoadSession(realPath)
	if err != nil {
		// The lease already moved to the target; re-point it at the session the
		// controller still owns (best-effort).
		_ = s.rebindSessionLease(s.ctl().SessionPath())
		return http.StatusBadRequest, fmt.Errorf("load session: %w", err)
	}
	if hook := resumeBindHookForTest; hook != nil {
		hook()
	}
	if err := s.resumeCompositionInto(loaded, realPath); err != nil {
		// The turn in flight owns the session it is writing to. Binding another
		// one under it is how the running conversation's output lands in the
		// transcript the user just switched to.
		_ = s.rebindSessionLease(s.ctl().SessionPath())
		return http.StatusConflict, err
	}
	if ctrl, ok := s.ctl().(*control.Controller); ok && s.leases != nil {
		if err := s.leases.BindControllerAuthority(ctrl); err != nil {
			return http.StatusInternalServerError, refusal(http.StatusInternalServerError, "session.bind_failed", errors.New("session authority: unable to bind resumed session"), nil)
		}
	}
	if settings != nil {
		if s.branchModelAvailable(settings.Model) {
			if currentModelRef(s.ctl()) != settings.Model {
				if err := s.switchModelLocked(context.Background(), settings.Model); err != nil {
					return http.StatusInternalServerError, err
				}
			}
		} else {
			s.notifyBranchModelFallback(settings.Model)
		}
		// A model rebuild constructs a new controller, so apply these after it.
		s.ctl().SetAgentPreset(settings.AgentPreset)
		s.ctl().SetPlanMode(settings.Mode == "plan")
		if s.leases == nil || s.leases.Lease() != nil {
			if err := s.ctl().Snapshot(); err != nil {
				return http.StatusInternalServerError, err
			}
		}
	} else {
		s.adoptSessionModelLocked(realPath)
	}
	s.resetSessionView()
	return http.StatusNoContent, nil
}

// pathUnresolved tells a path that is not there from one the host could not
// read. The host is the only party that saw which it was, so it says so: a
// transcript that is gone is "no such session", anything else carries the
// operating system's own reason for the person or support to read.
func pathUnresolved(err error) (int, error) {
	if errors.Is(err, fs.ErrNotExist) {
		return http.StatusNotFound, refusal(http.StatusNotFound, codeSessionUnknown, err, nil)
	}
	return http.StatusBadRequest, refusal(http.StatusBadRequest, codeSessionPathUnreadable, err, map[string]any{"detail": err.Error()})
}

// adoptSessionModelLocked rebuilds the runtime on the model the target session
// last ran on, once the resume and its lease have landed. A session another
// runtime holds cannot be rebound, so it stays on the current model.
// A pane resolving through the hub has no local catalog to check the ref against.
func (s *Server) adoptSessionModelLocked(path string) {
	if s.resolver != nil {
		return
	}
	cur := s.ctl()
	cfg, err := runtimeConfig(cur)
	if err != nil {
		return
	}
	ref := boot.ModelForResume("", path, cfg)
	if ref == "" {
		return
	}
	if target, ok := runtimeTargetForModel(cur, ref); ok && runtimeSelectionMatches(cur, target) {
		return
	}
	if err := s.switchModelLocked(context.Background(), ref); err != nil {
		slog.Warn("serve: restore session model failed", "ref", ref)
	}
}
