package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"reasonix/internal/event"
)

func (c *Controller) prepareTurnAdmission(body func(context.Context) error) func(context.Context) error {
	return c.prepareTurnAdmissionWithGoalRound(context.Background(), body, nil)
}

func (c *Controller) prepareTurnAdmissionWithGoalRound(admissionCtx context.Context, body func(context.Context) error, goalRound *goalRoundReservation) func(context.Context) error {
	if admissionCtx == nil {
		admissionCtx = context.Background()
	}
	admissionErr := c.turnEventLedgerError()
	ledger := c.turnEventLedger()
	if admissionErr == nil && goalRound != nil && ledger == nil {
		admissionErr = errors.New("goal round admission requires the v3 turn ledger")
	}
	if admissionErr == nil && ledger != nil {
		if ledger.CurrentStatus() == event.TurnRecoveryRequired {
			admissionErr = ErrRecoveryRequired
		} else if id, err := ledger.Begin(); err != nil {
			admissionErr = err
		} else {
			c.mu.Lock()
			c.turns.turnID = id
			c.mu.Unlock()
			if err := c.emitTurnEventChecked(event.Event{Kind: event.TurnStatusChanged, Status: event.TurnQueued}); err != nil {
				admissionErr = err
			} else if goalRound != nil {
				admissionErr = c.commitGoalRoundAdmission(goalRound)
			} else if err := c.emitTurnEventChecked(event.Event{Kind: event.TurnStarted, Status: event.TurnInProgress}); err != nil {
				admissionErr = err
			} else if c.executor != nil {
				// The committed host turn boundary owns todo lifetime. The executor
				// repeats this reset on entry for controller-less clients.
				c.executor.BeginTurnTodoState()
			}
		}
	}
	if admissionErr == nil {
		admissionErr = c.flushSubmissionAdmission(admissionCtx)
	}
	if admissionErr == nil {
		if c.executor != nil && goalRound != nil {
			c.executor.BeginTurnTodoState()
		}
		return body
	}
	slog.Error("controller: persist turn admission", "err", admissionErr)
	return func(context.Context) error { return fmt.Errorf("persist turn admission: %w", admissionErr) }
}
