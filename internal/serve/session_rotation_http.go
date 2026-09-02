package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

func (s *Server) planDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string                     `json:"id"`
		Action   control.PlanDecisionAction `json:"action"`
		Feedback string                     `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := s.ctl().ResolvePlanDecisionWithFeedback(body.ID, body.Action, body.Feedback); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	s.ctl().SetPlanMode(body.On)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCommand(w, r, false)
}

func (s *Server) clearSessionFromSubmit(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCommand(w, r, true)
}

func (s *Server) clearSessionCommand(w http.ResponseWriter, r *http.Request, emitNotice bool) {
	// Clear rotates the session path just like /new, but also removes the old
	// transcript artifacts. Keep controller mutation and lease rebinding under
	// one binding lock so remote clients never observe split ownership.
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if !s.validateExpectedSessionLocked(w, r) {
		return
	}
	if err := s.ctl().ClearSession(); err != nil {
		if control.IsSessionRotationBusy(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ctrl, ok := s.ctl().(*control.Controller); ok {
		ctrl.EnsureSessionPath()
		s.setControllerPath(ctrl, ctrl.SessionPath())
	}
	s.bc.ResetSessionPath(s.ctl().SessionPath())
	if err := s.rebindSessionLease(s.ctl().SessionPath()); err != nil {
		http.Error(w, sessionInUseError(err), http.StatusConflict)
		return
	}
	path := s.ctl().SessionPath()
	w.Header().Set(sessionPathHeader, path)
	s.announceSessionChanged(path, true)
	if emitNotice {
		s.bc.Emit(event.Event{Kind: event.Notice, Text: "context cleared", SessionPath: path})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) newSession(w http.ResponseWriter, r *http.Request) {
	s.newSessionCommand(w, r, false)
}

func (s *Server) newSessionFromSubmit(w http.ResponseWriter, r *http.Request) {
	s.newSessionCommand(w, r, true)
}

func (s *Server) newSessionCommand(w http.ResponseWriter, r *http.Request, emitNotice bool) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if !s.validateExpectedSessionLocked(w, r) {
		return
	}
	cur := s.ctl()
	if controllerHasActiveRuntimeWork(cur) {
		curCtrl, ok := cur.(*control.Controller)
		if !ok {
			http.Error(w, "cannot start a new session while active work or background jobs are running", http.StatusConflict)
			return
		}
		if err := s.busyDetach(r.Context(), curCtrl, "", nil); err != nil {
			s.renderBindError(w, err)
			return
		}
		path := s.ctl().SessionPath()
		w.Header().Set(sessionPathHeader, path)
		s.announceSessionChanged(path, true)
		if emitNotice {
			s.bc.Emit(event.Event{Kind: event.Notice, Text: "new session", SessionPath: path})
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := cur.NewSession(); err != nil {
		if control.IsSessionRotationBusy(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ctrl, ok := cur.(*control.Controller); ok {
		ctrl.EnsureSessionPath()
		s.setControllerPath(ctrl, ctrl.SessionPath())
	}
	s.bc.ResetSessionPath(cur.SessionPath())
	if err := s.rebindSessionLease(cur.SessionPath()); err != nil {
		http.Error(w, sessionInUseError(err), http.StatusConflict)
		return
	}
	path := cur.SessionPath()
	w.Header().Set(sessionPathHeader, path)
	s.announceSessionChanged(path, true)
	if emitNotice {
		s.bc.Emit(event.Event{Kind: event.Notice, Text: "new session", SessionPath: path})
	}
	w.WriteHeader(http.StatusNoContent)
}
