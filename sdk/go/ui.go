package extension

import (
	"context"
	"encoding/json"
	"strings"
)

func uiResultBoolPresent(raw json.RawMessage, key string) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return false
	}
	var value *bool
	return json.Unmarshal(object[key], &value) == nil && value != nil
}

func (s *server) handleUIAction(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.opts.UI.Action == nil {
		return nil, MustProtocolError(ErrUnknownMethod)
	}
	var p UIActionParams
	if err := strictDecode(raw, &p); err != nil || strings.TrimSpace(p.ActionID) == "" || strings.TrimSpace(p.SessionID) == "" {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if err := s.opts.UI.Action(ctx, p.ActionID, p.Args); err != nil {
		return UIActionResult{Accepted: false, Message: err.Error()}, nil
	}
	return UIActionResult{Accepted: true}, nil
}

func (s *server) handleUISubmit(ctx context.Context, raw json.RawMessage) (any, error) {
	if s.opts.UI.Submit == nil {
		return nil, MustProtocolError(ErrUnknownMethod)
	}
	var p UISubmitParams
	if err := strictDecode(raw, &p); err != nil || strings.TrimSpace(p.SurfaceID) == "" ||
		strings.TrimSpace(p.SessionID) == "" || p.Values == nil {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	if err := s.opts.UI.Submit(ctx, p.SurfaceID, p.Values); err != nil {
		s.log.Printf("extension: UI submit for surface %q failed: %v", p.SurfaceID, err)
		return UISubmitResult{Accepted: false}, nil
	}
	return UISubmitResult{Accepted: true}, nil
}
