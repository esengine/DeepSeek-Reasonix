package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
)

type managedRecoveryKey struct{}

// WithManagedRecovery gives the caller sole ownership of retries. It does not
// change wire bytes and is intentionally opt-in for standalone provider users.
func WithManagedRecovery(ctx context.Context) context.Context {
	return context.WithValue(ctx, managedRecoveryKey{}, true)
}
func ManagedRecovery(ctx context.Context) bool {
	v, _ := ctx.Value(managedRecoveryKey{}).(bool)
	return v
}

// RecoveryFailure is local diagnostic state, never part of a model request.
type RecoveryFailure struct {
	Phase      string
	Status     int
	Code       string
	RetryAfter time.Duration
	Retryable  bool
}

func ClassifyRecovery(err error) RecoveryFailure {
	f := RecoveryFailure{Phase: "unknown"}
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return f
	}
	if q := AsQuotaError(err); q != nil {
		f.Phase, f.Status, f.Code = "quota", q.Status, q.Code
		return f
	}
	var auth *AuthError
	if errors.As(err, &auth) {
		f.Phase = "auth"
		return f
	}
	if AsContextLimitError(err) != nil || AsOutputLimitError(err) != nil {
		f.Phase = "limit"
		return f
	}
	if AsReasoningReplayError(err) != nil {
		f.Phase = "protocol"
		return f
	}
	var api *APIError
	if errors.As(err, &api) {
		f.Phase, f.Status, f.RetryAfter = "headers", api.Status, api.RetryAfter
		var body struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(api.Body), &body) == nil {
			f.Code = body.Error.Code
			if f.Code == "" {
				f.Code = body.Error.Type
			}
		}
		f.Retryable = RetryableStatus(api.Status) || api.Status == 409
		if api.ShouldRetry == "false" {
			f.Retryable = false
		}
		return f
	}
	if IsStreamInterrupted(err) {
		f.Phase, f.Retryable = "stream", true
		return f
	}
	if errors.Is(err, ErrEmptyResponse) {
		f.Phase, f.Retryable = "empty", true
		return f
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || IsConnReset(err) {
		f.Phase, f.Retryable = "connect", true
	}
	return f
}
