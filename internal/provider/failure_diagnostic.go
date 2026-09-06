package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// FailureDiagnostic contains safe classification only, never response bodies.
type FailureDiagnostic struct {
	Kind    string `json:"kind"`
	Status  int    `json:"status,omitempty"`
	TraceID string `json:"traceId,omitempty"`
}

func DiagnoseFailure(err error) *FailureDiagnostic {
	if err == nil {
		return nil
	}
	d := &FailureDiagnostic{Kind: "unknown"}
	var api *APIError
	if errors.As(err, &api) {
		d.Status = api.Status
		// Trace identifiers are opaque tokens, not arbitrary header text.
		if len(api.TraceID) <= 128 && strings.IndexFunc(api.TraceID, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r))
		}) < 0 {
			d.TraceID = api.TraceID
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		d.Kind = "cancelled"
	case AsQuotaError(err) != nil:
		d.Kind = "quota"
		d.Status = AsQuotaError(err).Status
	case errors.As(err, new(*AuthError)):
		d.Kind = "auth"
		var auth *AuthError
		if errors.As(err, &auth) {
			d.Status = auth.Status
		}
	case AsContextLimitError(err) != nil || AsOutputLimitError(err) != nil:
		d.Kind = "limit"
	case AsReasoningReplayError(err) != nil:
		d.Kind = "protocol"
	case IsOpaqueBadRequest(err):
		d.Kind = "upstream_reason_missing"
	case ClassifyRecovery(err).Retryable:
		d.Kind = "temporary"
	case api != nil && api.Status >= 400 && api.Status < 500:
		d.Kind = "request"
	}
	return d
}

// IsOpaqueBadRequest deliberately recognizes only empty/model-only 400 bodies.
// An unknown structured error may carry a useful reason and must not be guessed.
func IsOpaqueBadRequest(err error) bool {
	var api *APIError
	if !errors.As(err, &api) || api.Status != 400 {
		return false
	}
	if strings.TrimSpace(api.Body) == "" {
		return true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(api.Body), &object) != nil || object == nil {
		return false
	}
	for key := range object {
		if key != "model" {
			return false
		}
	}
	return true
}
