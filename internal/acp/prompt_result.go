package acp

import "log/slog"

func promptStopReason(runErr error, cancelled, recoveryPaused bool, sessionID string) (StopReason, error) {
	if cancelled {
		return StopCancelled, nil
	}
	if runErr == nil {
		return StopEndTurn, nil
	}
	if recoveryPaused {
		return StopEndTurn, nil
	}
	reason := clipStatusError(runErr, 2_048)
	slog.Error("acp: session/prompt failed", "session_id", sessionID, "err", reason)
	return "", &RPCError{Code: ErrInternal, Message: "session/prompt: " + reason}
}
