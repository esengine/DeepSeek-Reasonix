package acp

import (
	"errors"
	"log/slog"

	"reasonix/internal/agent"
	"reasonix/internal/event"
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
	var completionPause *agent.CompletionUncertainError
	if errors.As(runErr, &completionPause) {
		return StopEndTurn, clipStatusError(runErr, 2_048), nil
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

func promptPauseNotice(runErr error, text string) event.Event {
	notice := event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text}
	var completionPause *agent.CompletionUncertainError
	if errors.As(runErr, &completionPause) {
		notice.Level = event.LevelInfo
		notice.Code = event.NoticeCodeCompletionUncertain
	}
	return notice
}
