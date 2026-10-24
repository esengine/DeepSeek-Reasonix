package control

import (
	"context"
	"encoding/json"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
	"reasonix/internal/session"
)

// Both synchronous and asynchronous turns publish through the same boundary.
// Keep failure classification there so live and reopened notices agree.
func withTerminalFailureDiagnostic(e event.Event) event.Event {
	if e.Kind == event.TurnDone {
		if e.Diagnostic == nil {
			cause := e.Err
			if e.Cancelled {
				cause = context.Canceled
			}
			e.Diagnostic = provider.DiagnoseFailure(cause)
		}
		if e.Detail == "" {
			e.Detail = secrets.FailureDetail(e.Err)
			if metadata := provider.FailureDiagnosticDetail(e.Diagnostic); metadata != "" {
				if e.Detail != "" {
					e.Detail += "\n"
				}
				e.Detail += metadata
			}
		}
	}
	return e
}

// One bounded, optional record shares the terminal commit. Cold exports can
// inspect it without a resident controller; older readers skip it safely.
func (c *Controller) providerDiagnosticEvent(e event.Event) (*session.Event, error) {
	status := e.Status
	if status == "" {
		status = terminalTurnStatus(e)
	}
	if status != event.TurnFailed && status != event.TurnInterrupted && status != event.TurnRecoveryRequired {
		return nil, nil
	}
	failure := e.Diagnostic
	if failure == nil {
		failure = provider.DiagnoseFailure(e.Err)
	}
	requests, dropped, truncated := c.providerDiagnosticTurnSnapshot(e.TurnID)
	if len(requests) == 0 && failure == nil && (dropped == nil || *dropped == 0) {
		return nil, nil
	}
	payload, err := json.Marshal(struct {
		SchemaVersion int                         `json:"schemaVersion"`
		Failure       *provider.FailureDiagnostic `json:"failure,omitempty"`
		RequestLimit  int                         `json:"requestLimit"`
		Requests      []providerDiagnostic        `json:"requests"`
		Dropped       *uint64                     `json:"dropped,omitempty"`
		Truncated     bool                        `json:"truncated"`
	}{2, failure, 128, requests, dropped, truncated})
	if err != nil {
		return nil, err
	}
	payload, err = secrets.RedactJSON(payload)
	if err != nil {
		return nil, err
	}
	return &session.Event{Kind: "diagnostic/provider", Optional: true, Payload: payload}, nil
}
