package acp

import (
	"context"
	"encoding/json"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/sessioninbox"
)

func (s *service) sessionAPI(sess *acpSession) (control.SessionAPI, error) {
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "unknown session"}
	}
	ctrl := sess.currentCtrl()
	api, ok := ctrl.(control.SessionAPI)
	if !ok {
		return nil, &RPCError{Code: ErrInternal, Message: "session controller does not expose inbox"}
	}
	if ensurer, ok := any(api).(interface{ EnsureSessionPath() }); ok {
		ensurer.EnsureSessionPath()
	}
	return api, nil
}

func (s *service) sessionInboxEnqueue(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionInboxEnqueueParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionInboxEnqueueMethod + ": " + err.Error()}
	}
	sess := s.session(p.SessionID)
	api, err := s.sessionAPI(sess)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(p.Text)
	if text == "" {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionInboxEnqueueMethod + ": empty text"}
	}
	intent := sessioninbox.IntentFollowup
	if strings.EqualFold(p.Intent, "steer") {
		intent = sessioninbox.IntentSteer
	}
	req := control.InboxRequest{
		Intent:      intent,
		Display:     text,
		Raw:         text,
		Submit:      text,
		Source:      "acp",
		Idempotency: strings.TrimSpace(p.IdempotencyKey),
	}
	var rec sessioninbox.InboxReceipt
	if intent == sessioninbox.IntentSteer {
		rec, err = api.TryEnqueueAndSteer(req)
	} else {
		// ACP owns a blocking session/prompt response boundary. Keep follow-ups
		// passive here so the prompt handler drains them with the same sink;
		// detached Controller dispatch would race the transition between turns.
		rec, err = api.EnqueueInbox(req)
	}
	if err != nil {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: err.Error()}
	}
	return map[string]any{
		"itemId":      rec.ItemID,
		"disposition": string(rec.Disposition),
		"position":    rec.Position,
		"paused":      rec.Paused,
		"idempotent":  rec.Idempotent,
	}, nil
}

func (s *service) sessionInboxList(_ context.Context, raw json.RawMessage) (any, error) {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	api, err := s.sessionAPI(s.session(p.SessionID))
	if err != nil {
		return nil, err
	}
	return api.InboxSnapshot(), nil
}

func (s *service) sessionInboxGet(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionInboxItemParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	api, err := s.sessionAPI(s.session(p.SessionID))
	if err != nil {
		return nil, err
	}
	meta, env, err := api.ReadInboxItem(p.ItemID)
	if err != nil {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: err.Error()}
	}
	return map[string]any{"meta": meta, "envelope": env}, nil
}

func (s *service) sessionInboxUpdate(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionInboxUpdateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	api, err := s.sessionAPI(s.session(p.SessionID))
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(p.Text)
	meta, err := api.UpdateInboxItem(p.ItemID, text, text, text)
	if err != nil {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: err.Error()}
	}
	return meta, nil
}

func (s *service) sessionInboxDelete(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionInboxItemParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	api, err := s.sessionAPI(s.session(p.SessionID))
	if err != nil {
		return nil, err
	}
	if err := api.DeleteInboxItem(p.ItemID); err != nil {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: err.Error()}
	}
	return map[string]any{"ok": true}, nil
}

func (s *service) sessionInboxMove(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionInboxMoveParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	api, err := s.sessionAPI(s.session(p.SessionID))
	if err != nil {
		return nil, err
	}
	if err := api.MoveInboxItem(p.ItemID, p.ToIndex); err != nil {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: err.Error()}
	}
	return map[string]any{"ok": true}, nil
}

func (s *service) sessionInboxSetPaused(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionInboxPauseParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	api, err := s.sessionAPI(s.session(p.SessionID))
	if err != nil {
		return nil, err
	}
	if err := api.SetInboxPaused(p.Paused); err != nil {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: err.Error()}
	}
	return map[string]any{"paused": p.Paused}, nil
}

func (s *service) sessionInboxRetry(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionInboxItemParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	api, err := s.sessionAPI(s.session(p.SessionID))
	if err != nil {
		return nil, err
	}
	if err := api.RetryInboxItem(p.ItemID); err != nil {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: err.Error()}
	}
	return map[string]any{"ok": true}, nil
}

func (s *service) sessionInboxRefresh(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionInboxItemParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: err.Error()}
	}
	api, err := s.sessionAPI(s.session(p.SessionID))
	if err != nil {
		return nil, err
	}
	if err := api.RefreshInboxReferences(p.ItemID); err != nil {
		return nil, &RPCError{Code: ErrInvalidRequest, Message: err.Error()}
	}
	return map[string]any{"ok": true}, nil
}
