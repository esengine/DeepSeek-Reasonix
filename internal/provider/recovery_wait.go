package provider

import (
	"errors"
	"fmt"
	"time"
)

// RecoveryWaitExhaustedError ends managed waiting once the total wait budget
// is spent, so a turn on an unreachable provider fails instead of hanging.
type RecoveryWaitExhaustedError struct {
	Phase    string
	Code     string
	Status   int
	Waited   time.Duration
	Attempts int
	Cause    error
}

func (e *RecoveryWaitExhaustedError) Error() string {
	msg := fmt.Sprintf("provider unreachable for %s (%s)", e.Waited.Round(time.Second), e.Phase)
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

func (e *RecoveryWaitExhaustedError) Unwrap() error { return e.Cause }

func AsRecoveryWaitExhausted(err error) *RecoveryWaitExhaustedError {
	var exhausted *RecoveryWaitExhaustedError
	if errors.As(err, &exhausted) {
		return exhausted
	}
	return nil
}
