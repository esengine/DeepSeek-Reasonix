package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"reasonix/internal/session/control"
	"reasonix/internal/state/sessioninbox"
)

// Implementation-defined JSON-RPC codes for an inbox refusal. The identities
// under data.code are the HTTP frontend's; these two codes say which of the two
// answers a client must act on — a call it may repair and make again, or one
// whose work may already have run.
const (
	ErrInboxRefused    = -32020
	ErrInboxMayHaveRun = -32021
)

// InboxRefusalData is the data member of an inbox refusal: the same dotted
// identity the HTTP frontend sends, so one vocabulary names both surfaces.
type InboxRefusalData struct {
	Code string `json:"code"`
}

// refuseInbox answers one inbox call with the documented pair: the JSON-RPC
// code for the class, data.code for the condition, and the kernel's English as
// the message, which clients still show.
func refuseInbox(rpcCode int, dataCode, message string) *RPCError {
	return &RPCError{Code: rpcCode, Message: message, Data: InboxRefusalData{Code: dataCode}}
}

// inboxRefusal names every condition the queue can refuse an inbox call with.
// A sentinel with no case here reached the client as "your request was
// malformed", so a client could not tell a gone item from a broken frame, nor
// stop itself retrying work that may already have run. repolint reads this
// switch against the store's exported family, the way it reads the HTTP writer.
func inboxRefusal(err error) *RPCError {
	switch {
	case errors.Is(err, control.ErrSteerApplied):
		return refuseInbox(ErrInboxRefused, "steer.already_applied", err.Error())
	case errors.Is(err, sessioninbox.ErrItemTooLarge):
		return refuseInbox(ErrInboxRefused, "inbox.item_too_large", err.Error())
	case errors.Is(err, sessioninbox.ErrCapacityItems):
		return refuseInbox(ErrInboxRefused, "inbox.capacity_items", err.Error())
	case errors.Is(err, sessioninbox.ErrCapacityBytes):
		return refuseInbox(ErrInboxRefused, "inbox.capacity_bytes", err.Error())
	case errors.Is(err, sessioninbox.ErrInvalidState):
		return refuseInbox(ErrInboxRefused, "inbox.invalid_state", err.Error())
	case errors.Is(err, sessioninbox.ErrRetryMayRepeat):
		// Raised precisely when the item's block code is not RetrySafe, which is
		// the one answer a client must never retry blindly.
		return refuseInbox(ErrInboxMayHaveRun, "inbox.retry_may_repeat", err.Error())
	case errors.Is(err, sessioninbox.ErrForeignRun):
		return refuseInbox(ErrInboxRefused, "inbox.foreign_run", err.Error())
	case errors.Is(err, sessioninbox.ErrPaused):
		return refuseInbox(ErrInboxRefused, "inbox.paused", err.Error())
	case errors.Is(err, sessioninbox.ErrUnrecovered):
		return refuseInbox(ErrInboxRefused, "inbox.unrecovered", err.Error())
	case errors.Is(err, sessioninbox.ErrNotFound):
		return refuseInbox(ErrInboxRefused, "inbox.not_found", err.Error())
	case errors.Is(err, sessioninbox.ErrIdempotencyConflict):
		return refuseInbox(ErrInboxRefused, "inbox.idempotency_conflict", err.Error())
	case errors.Is(err, sessioninbox.ErrSchemaReadonly):
		return refuseInbox(ErrInboxRefused, "inbox.schema_readonly", err.Error())
	case errors.Is(err, sessioninbox.ErrClosed):
		return refuseInbox(ErrInboxRefused, "inbox.closed", err.Error())
	case errors.Is(err, sessioninbox.ErrEmpty):
		return refuseInbox(ErrInvalidParams, "inbox.empty", err.Error())
	default:
		// Not a refusal this surface can name: the queue or the session's path
		// to it failed. That is a fault, and saying so is more honest than
		// blaming the request.
		return &RPCError{Code: ErrInternal, Message: err.Error()}
	}
}

func (s *service) sessionAPI(sess *acpSession) (control.EditorAPI, error) {
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "unknown session"}
	}
	ctrl := sess.currentCtrl()
	api, ok := ctrl.(control.EditorAPI)
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
		return nil, refuseInbox(ErrInvalidParams, "inbox.empty", sessionInboxEnqueueMethod+": empty text")
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
		return nil, inboxRefusal(err)
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
		return nil, inboxRefusal(err)
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
		return nil, inboxRefusal(err)
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
		return nil, inboxRefusal(err)
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
		return nil, inboxRefusal(err)
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
		return nil, inboxRefusal(err)
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
		return nil, inboxRefusal(err)
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
		return nil, inboxRefusal(err)
	}
	return map[string]any{"ok": true}, nil
}
