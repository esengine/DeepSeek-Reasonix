package acp

import (
	"context"
	"encoding/json"

	"reasonix/internal/session/control"
)

const (
	mcpStatusSchemaVersion       = 1
	sessionMCPStatusMethod       = "_reasonix.io/session/mcpStatus"
	sessionMCPStatusUpdateMethod = "_reasonix.io/session/mcpStatus_update"
)

func registerSessionHealthMethods(conn *Conn, s *service) {
	conn.Handle(sessionStatusMethod, s.sessionStatus)
	conn.Handle(sessionMCPStatusMethod, s.sessionMCPStatus)
}

func (s *service) bindSessionHealthEvents(sess *acpSession) {
	s.bindStatusEvents(sess)
	s.bindMCPStatusEvents(sess)
}

func (s *updateSink) emitMCPStatus() {
	s.mu.Lock()
	publish := s.mcpStatus
	s.mu.Unlock()
	if publish != nil {
		publish()
	}
}

// ReasonixMCPStatus is a session-local snapshot; no probe is started by reading
// it. A server in standby has callable cached tools but no live connection yet.
type ReasonixMCPStatus struct {
	SchemaVersion int                 `json:"schemaVersion"`
	SessionID     string              `json:"sessionId"`
	Servers       []ReasonixMCPServer `json:"servers"`
}

type ReasonixMCPServer struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Tools      int    `json:"tools"`
}

func mcpStatusSnapshot(id string, rows []control.MCPHealth) ReasonixMCPStatus {
	servers := make([]ReasonixMCPServer, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, ReasonixMCPServer{
			Name: row.Name, Status: row.Status, Error: row.Error,
			HTTPStatus: row.HTTPStatus, Tools: row.Tools,
		})
	}
	return ReasonixMCPStatus{SchemaVersion: mcpStatusSchemaVersion, SessionID: id, Servers: servers}
}

func (s *service) sessionMCPStatus(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionStatusParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionMCPStatusMethod + ": " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionMCPStatusMethod + ": unknown session " + p.SessionID}
	}
	return mcpStatusSnapshot(p.SessionID, sess.currentCtrl().MCPServerHealth()), nil
}

func (s *service) publishMCPStatus(sess *acpSession) {
	if sess == nil {
		return
	}
	_ = s.conn.Notify(sessionMCPStatusUpdateMethod,
		mcpStatusSnapshot(sess.id, sess.currentCtrl().MCPServerHealth()))
}

func (s *service) bindMCPStatusEvents(sess *acpSession) {
	if sess != nil && sess.sink != nil {
		sess.sink.bindMCPStatus(func() { s.publishMCPStatus(sess) })
	}
}
