package serve

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"reasonix/internal/agent"
)

const (
	sessionPathHeader           = "X-Reasonix-Session-Path"
	expectedSessionPathHeader   = "X-Reasonix-Expected-Session-Path"
	expectedModelSettingsHeader = "X-Reasonix-Expected-Model-Settings"
	foregroundMutationMaxBody   = 8 << 20
)

var errExpectedSessionChanged = errors.New("active session changed; retry on the current session")

// expectedSessionErrorLocked fences a foreground mutation to the session the
// caller displayed when it issued the request. The header is optional for
// compatibility with browser clients and older Desktop builds. bindMu must be
// held so validation and controller use share one publication epoch.
func (s *Server) expectedSessionErrorLocked(r *http.Request) error {
	return s.expectedSessionPathErrorLocked(r.Header.Get(expectedSessionPathHeader))
}

func (s *Server) expectedSessionPathErrorLocked(rawExpected string) error {
	expected := agent.CanonicalSessionPath(strings.TrimSpace(rawExpected))
	if expected == "" {
		return nil
	}
	actual := agent.CanonicalSessionPath(strings.TrimSpace(s.ctl().SessionPath()))
	if actual != expected {
		return errExpectedSessionChanged
	}
	return nil
}

// expectedSessionIsSpectatorPinLocked reports whether the caller's expected
// session is one a local runtime owns (a spectator pin). Such a caller is
// deliberately viewing a non-foreground session; write commands to it must be
// refused with the takeover wording, while foreground-switch commands may pass
// (validated by validateSwitchExpectedLocked).
func (s *Server) expectedSessionIsSpectatorPinLocked(r *http.Request) bool {
	return s.sessionMirrored(r.Header.Get(expectedSessionPathHeader))
}

func (s *Server) validateExpectedSessionLocked(w http.ResponseWriter, r *http.Request) bool {
	if expected := r.Header.Get(expectedModelSettingsHeader); expected != "" {
		snapshot, ok := s.ctl().(interface{ ModelSettingsSourceRevision() string })
		if !ok || snapshot.ModelSettingsSourceRevision() != expected {
			http.Error(w, "session model settings changed; apply the latest saved settings before starting this run", http.StatusConflict)
			return false
		}
	}
	if err := s.expectedSessionErrorLocked(r); err != nil {
		// A spectator pinned to a local-owned session is not misrouted — it is
		// read-only by ownership. Answer with the takeover wording instead of
		// the generic "active session changed".
		if s.expectedSessionIsSpectatorPinLocked(r) {
			http.Error(w, errSessionTakenOver, http.StatusConflict)
			return false
		}
		http.Error(w, err.Error(), http.StatusConflict)
		return false
	}
	return true
}

// validateSwitchExpectedLocked is the fence for foreground-switch commands
// (/new, /clear, /resume). A spectator pinned to a local-owned session is
// allowed to switch: it is leaving its read-only pin for a real foreground
// session, which is the only way "the remote" regains the ability to act.
func (s *Server) validateSwitchExpectedLocked(w http.ResponseWriter, r *http.Request) bool {
	if err := s.expectedSessionErrorLocked(r); err != nil {
		if s.expectedSessionIsSpectatorPinLocked(r) {
			return true
		}
		http.Error(w, err.Error(), http.StatusConflict)
		return false
	}
	return true
}

func (s *Server) foregroundMutation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !bufferForegroundMutationBody(w, r) {
			return
		}
		s.bindMu.Lock()
		defer s.bindMu.Unlock()
		if !s.validateExpectedSessionLocked(w, r) {
			return
		}
		// A mirrored foreground is owned by a local runtime; every mutation
		// (submit-adjacent commands, inbox, approvals) is read-only-refused
		// until the remote side reclaims the session.
		if s.rejectMirroredForegroundLocked(w) {
			return
		}
		switch r.URL.Path {
		case "/goal/resume", "/compact", "/summarize":
			if err := s.refreshRunModelSettingsLocked(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			if !s.validateExpectedSessionLocked(w, r) {
				return
			}
		}
		next(w, r)
	}
}

// bufferForegroundMutationBody drains the bounded JSON body before acquiring
// bindMu. An authenticated client that uploads slowly can occupy its own
// handler, but cannot freeze every foreground command and session transition.
func bufferForegroundMutationBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return true
	}
	limited := http.MaxBytesReader(w, r.Body, foregroundMutationMaxBody)
	body, err := io.ReadAll(limited)
	_ = limited.Close()
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
		}
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return true
}
