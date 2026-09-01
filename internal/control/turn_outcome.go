package control

import (
	"errors"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

// turnOutcome maps a finished run's typed pause onto the TurnDone outcome
// string. Empty means an ordinary success or failure.
func turnOutcome(err error) string {
	var readinessErr *agent.FinalReadinessError
	if errors.As(err, &readinessErr) {
		return event.TurnOutcomeFinalReadiness
	}
	var pauseErr *agent.RecoveryPauseError
	if errors.As(err, &pauseErr) {
		return event.TurnOutcomeRecoveryPaused
	}
	var completionErr *agent.CompletionUncertainError
	if errors.As(err, &completionErr) {
		return event.TurnOutcomeCompletionUncertain
	}
	return ""
}
