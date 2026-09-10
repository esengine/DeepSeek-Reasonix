package browser

import (
	"errors"
	"fmt"

	"reasonix/internal/tool"
)

const (
	noBrowserText = "blocked: no browser is attached to this session, so browser tools are unavailable; do not retry."
	noGrantText   = "blocked: this task holds no browser grant. The browser is unavailable until the host grants access; do not retry."
	staleText     = "blocked: stale reference - the page changed since that snapshot. Call browser_snapshot again and act with its documentToken and a fresh operationId."
	takenOverText = "blocked: the user took over this tab, which invalidated every earlier reference. Wait for the user to finish, then call browser_snapshot again before acting."
)

// translate maps executor sentinels onto tool outcomes: refusals become
// blocked cards (never retried), a lost receipt becomes an error that
// forbids a retry, and anything else passes through unchanged.
func translate(err error, what string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrStaleReference):
		return tool.Blocked(staleText)
	case errors.Is(err, ErrTakenOver):
		return tool.Blocked(takenOverText)
	case errors.Is(err, ErrNoGrant):
		return tool.Blocked(noGrantText)
	case errors.Is(err, ErrUnknownOutcome):
		return unknownOutcome(what)
	}
	return err
}

func unknownOutcome(what string) error {
	return fmt.Errorf("outcome unknown: the browser did not confirm whether %s ran. It may or may not have taken effect, so it must not be retried with the same or a new operationId. Call browser_snapshot to observe the page before deciding what to do next.", what)
}

func notExecuted(what, reason string) error {
	if reason == "" {
		reason = "no reason reported"
	}
	return fmt.Errorf("not_executed: %s (%s). Take a new browser_snapshot and retry with its documentToken and a fresh operationId.", what, reason)
}
