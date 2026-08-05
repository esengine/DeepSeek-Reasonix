package cli

import (
	"context"
	"errors"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

type runCompletion struct {
	outcome string
	subtype string
	// class is the benchmark-facing failure taxonomy. It names which guard or
	// transport ended the run and never affects the exit code or wire outcome.
	class    string
	isError  bool
	exitCode int
}

func classifyRunCompletion(err error) runCompletion {
	if err == nil {
		return runCompletion{subtype: "success", class: "success"}
	}
	var pauseErr *agent.RecoveryPauseError
	if errors.As(err, &pauseErr) {
		return runCompletion{
			outcome:  event.TurnOutcomeRecoveryPaused,
			subtype:  event.TurnOutcomeRecoveryPaused,
			class:    event.TurnOutcomeRecoveryPaused,
			exitCode: 0,
		}
	}
	return runCompletion{
		subtype:  "error_during_execution",
		class:    runFailureClass(err),
		isError:  true,
		exitCode: 1,
	}
}

func runFailureClass(err error) string {
	if class := agent.PauseClass(err); class != "" {
		return class
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}
	return "error_during_execution"
}
