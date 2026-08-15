package acp

import (
	"errors"
	"log/slog"

	"reasonix/internal/agent"
)

// promptStopReason maps a finished controller run onto ACP v1. Controlled
// pauses remain successful prompt responses; genuine failures use JSON-RPC's
// error channel because ACP v1 has no error stop reason.
func promptStopReason(runErr error, cancelled bool, sessionID string) (StopReason, *agent.FinalReadinessError, error) {
	if cancelled {
		return StopCancelled, nil, nil
	}
	if runErr == nil {
		return StopEndTurn, nil, nil
	}

	var readinessErr *agent.FinalReadinessError
	if errors.As(runErr, &readinessErr) {
		return StopEndTurn, readinessErr, nil
	}
	var recoveryPause *agent.RecoveryPauseError
	if errors.As(runErr, &recoveryPause) {
		return StopEndTurn, nil, nil
	}

	reason := clipStatusError(runErr, 2_048)
	slog.Error("acp: session/prompt failed", "session_id", sessionID, "err", reason)
	return "", nil, &RPCError{Code: ErrInternal, Message: "session/prompt: " + reason}
}
