package acp

import (
	"errors"
	"log/slog"

	"reasonix/internal/agent"
)

// promptStopReason maps a finished controller run onto ACP v1. Controlled
// pauses remain successful prompt responses; genuine failures use JSON-RPC's
// error channel because ACP v1 has no error stop reason.
func promptStopReason(runErr error, cancelled bool, sessionID string) (StopReason, string, error) {
	if cancelled {
		return StopCancelled, "", nil
	}
	if runErr == nil {
		return StopEndTurn, "", nil
	}

	var readinessErr *agent.FinalReadinessError
	if errors.As(runErr, &readinessErr) {
		return StopEndTurn, finalReadinessNotice(readinessErr), nil
	}
	var recoveryPause *agent.RecoveryPauseError
	if errors.As(runErr, &recoveryPause) {
		return StopEndTurn, "", nil
	}
	if pause, ok := agent.InspectRunPause(runErr); ok {
		stop := StopEndTurn
		if pause.Kind == "max_steps" {
			stop = StopMaxTurnRequests
		}
		return stop, clipStatusError(runErr, 2_048), nil
	}

	reason := clipStatusError(runErr, 2_048)
	slog.Error("acp: session/prompt failed", "session_id", sessionID, "err", reason)
	return "", "", &RPCError{Code: ErrInternal, Message: "session/prompt: " + reason}
}
