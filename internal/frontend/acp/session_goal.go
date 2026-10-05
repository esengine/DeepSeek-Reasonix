// session_goal.go — explicit Goal pause/resume over ACP.
package acp

import (
	"context"
	"encoding/json"
)

// Goal lifecycle vendor methods. Resume re-enters a retained Goal with its
// objective, counters, todos and delivery checkpoint; it does not start a
// turn — the next session/prompt continues it. Pause takes effect at the next
// Goal continuation boundary and leaves the current turn to session/cancel.
const (
	sessionGoalResumeMethod  = "_reasonix.io/session/goal/resume"
	sessionGoalPauseMethod   = "_reasonix.io/session/goal/pause"
	sessionGoalSchemaVersion = 1
)

// Implementation-defined JSON-RPC codes a client tells apart without reading
// the message.
const (
	ErrGoalNotResumable = -32010
	ErrGoalNotRunning   = -32011
)

// SessionGoalParams addresses the session whose Goal a method acts on.
type SessionGoalParams struct {
	SessionID string `json:"sessionId"`
}

// SessionGoalResult is the Goal as the status mechanism reports it right
// after the change, which is also published as a status update.
type SessionGoalResult struct {
	Goal ReasonixStatusGoal `json:"goal"`
}

func registerGoalMethods(conn *Conn, svc *service) {
	conn.Handle(sessionGoalResumeMethod, svc.sessionGoalResume)
	conn.Handle(sessionGoalPauseMethod, svc.sessionGoalPause)
}

func goalCapabilities(meta map[string]any) map[string]any {
	meta[sessionGoalResumeMethod] = ReasonixSchemaCapability{SchemaVersion: sessionGoalSchemaVersion}
	meta[sessionGoalPauseMethod] = ReasonixSchemaCapability{SchemaVersion: sessionGoalSchemaVersion}
	return meta
}

func (s *service) goalSession(method string, raw json.RawMessage) (*acpSession, error) {
	var p SessionGoalParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: method + ": " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: method + ": unknown session " + p.SessionID}
	}
	return sess, nil
}

func (s *service) sessionGoalResume(_ context.Context, raw json.RawMessage) (any, error) {
	sess, err := s.goalSession(sessionGoalResumeMethod, raw)
	if err != nil {
		return nil, err
	}
	sess.stateChangeMu.Lock()
	ctrl := sess.currentCtrl()
	resumed := ctrl.ResumeGoal()
	if resumed {
		ctrl.SetPlanMode(false)
		sess.setGoalDraftMode(false)
	}
	sess.stateChangeMu.Unlock()
	if !resumed {
		return nil, &RPCError{Code: ErrGoalNotResumable, Message: sessionGoalResumeMethod + ": session has no resumable goal"}
	}
	return s.goalChanged(sess), nil
}

func (s *service) sessionGoalPause(_ context.Context, raw json.RawMessage) (any, error) {
	sess, err := s.goalSession(sessionGoalPauseMethod, raw)
	if err != nil {
		return nil, err
	}
	sess.stateChangeMu.Lock()
	paused := sess.currentCtrl().PauseGoal()
	sess.stateChangeMu.Unlock()
	if !paused {
		return nil, &RPCError{Code: ErrGoalNotRunning, Message: sessionGoalPauseMethod + ": session has no running goal"}
	}
	return s.goalChanged(sess), nil
}

// goalChanged drops the turn-outcome label (cancelled, failed) the status was
// showing in place of the controller's Goal state, which the change just replaced.
func (s *service) goalChanged(sess *acpSession) SessionGoalResult {
	sess.mu.Lock()
	telemetry := sess.status
	sess.mu.Unlock()
	if telemetry != nil {
		telemetry.clearGoalOverride()
	}
	s.emitModeDrift(sess)
	s.publishStatus(sess, "goal")
	return SessionGoalResult{Goal: sess.statusSnapshot().Goal}
}

func (t *statusTelemetry) clearGoalOverride() {
	t.mutate(func(t *statusTelemetry) { t.goalOverride = "" })
}
