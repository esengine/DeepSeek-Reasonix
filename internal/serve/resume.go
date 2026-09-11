// Session switching over HTTP: /resume binds this runtime to another
// transcript. It lives apart from serve.go because the swap has three owners to
// keep in step — the controller's rotation gate, the session lease, and the
// broadcaster's ledger — and getting them out of order is how one conversation
// ends up written into another.
package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/store"
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
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return http.StatusBadRequest, refusal(http.StatusBadRequest, codeSessionBadPath, errors.New("invalid session dir"), nil)
	}
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil || !store.IsSessionTranscriptName(filepath.Base(absPath)) {
		return http.StatusBadRequest, refusal(http.StatusBadRequest, codeSessionBadPath, errors.New("invalid session path"), nil)
	}
	// A transcript that is gone lands here: the delete took the file, and the
	// list the click came from was drawn before it did.
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return http.StatusBadRequest, refusal(http.StatusBadRequest, codeSessionBadPath, errors.New("invalid session path"), nil)
	}
	if realPath == realDir || !strings.HasPrefix(realPath, realDir+string(os.PathSeparator)) {
		return http.StatusForbidden, refusal(http.StatusForbidden, codeSessionOutside, errors.New("path outside session dir"), nil)
	}
	if agent.IsCleanupPending(realPath) {
		return http.StatusBadRequest, refusal(http.StatusBadRequest, "session.pending_cleanup", errors.New("session is pending cleanup"), nil)
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
	loaded, err := agent.LoadSession(realPath)
	if err != nil {
		// The lease already moved to the target; re-point it at the session the
		// controller still owns (best-effort).
		_ = s.rebindSessionLease(s.ctl().SessionPath())
		return http.StatusBadRequest, fmt.Errorf("load session: %w", err)
	}
	if hook := resumeBindHookForTest; hook != nil {
		hook()
	}
	if err := s.ctl().Resume(loaded, realPath); err != nil {
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
	s.bc.ResetSession()
	return http.StatusNoContent, nil
}
