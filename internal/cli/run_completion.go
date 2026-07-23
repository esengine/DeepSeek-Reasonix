package cli

import (
	"errors"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

type runCompletion struct {
	outcome  string
	subtype  string
	isError  bool
	exitCode int
}

func classifyRunCompletion(err error) runCompletion {
	if err == nil {
		return runCompletion{subtype: "success"}
	}
	var pauseErr *agent.RecoveryPauseError
	if errors.As(err, &pauseErr) {
		return runCompletion{
			outcome:  event.TurnOutcomeRecoveryPaused,
			subtype:  event.TurnOutcomeRecoveryPaused,
			exitCode: 0,
		}
	}
	return runCompletion{
		subtype:  "error_during_execution",
		isError:  true,
		exitCode: 1,
	}
}
