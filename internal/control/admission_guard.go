package control

import (
	"context"
	"errors"

	"reasonix/internal/event"
	"reasonix/internal/session"
)

// admissionResult classifies what runGuarded did with a turn body.
type admissionResult int

const (
	turnStarted admissionResult = iota
	turnParked
	turnDroppedRunning
	turnDroppedRotating
	turnDroppedClosed
	turnDroppedDraining // generation no longer published after rebuild
	turnDroppedWriteAuthority
	turnDroppedAuthentication
)

// runGuarded runs body under a fresh context, guarding concurrent turns.
// Finishing-window arrivals park instead of dropping (see admissionResult).
func (c *Controller) runGuarded(body func(ctx context.Context) error) admissionResult {
	return c.runGuardedWithAdmission(body, turnAdmission{})
}

func (c *Controller) runGuardedWithAdmission(body func(ctx context.Context) error, admission turnAdmission) admissionResult {
	return c.admitGuardedTurn(body, false, true, nil, nil, admission)
}

// runGuardedOrPark admits like runGuarded but parks the body while another
// turn is running instead of using the deliberately-silent running drop.
// Reserved for inputs that are the user's own words (the steer fallback):
// the FIFO drain in finishGuardedTurn delivers them the moment the current
// turn finishes.
func (c *Controller) runGuardedOrPark(body func(ctx context.Context) error) admissionResult {
	return c.admitGuardedTurn(body, true, true, nil, nil, turnAdmission{})
}

// runGuardedInbox admits a durable item without parking it in volatile memory.
// onStart runs after admission is reserved and before its goroutine can finish.
func (c *Controller) runGuardedInbox(body func(ctx context.Context) error, onStart func()) admissionResult {
	if !c.submissions.mu.TryLock() {
		return turnDroppedRunning
	}
	defer c.submissions.mu.Unlock()
	return c.admitGuardedTurn(body, false, false, onStart, nil, turnAdmission{})
}

func (c *Controller) runGuardedGoalRound(reservation *goalRoundReservation, body func(ctx context.Context) error) admissionResult {
	c.submissions.mu.Lock()
	defer c.submissions.mu.Unlock()
	return c.admitGuardedTurn(body, false, false, nil, reservation, turnAdmission{})
}

func (c *Controller) admitGuardedTurn(body func(ctx context.Context) error, parkWhileRunning, parkWhileFinishing bool, onStart func(), goalRound *goalRoundReservation, admission turnAdmission) admissionResult {
	if err := c.authentication.admissionError(); err != nil {
		var authErr *AuthenticationError
		_ = errors.As(err, &authErr)
		code := "authentication_not_ready"
		if authErr != nil && authErr.State.Code != "" {
			code = authErr.State.Code
		}
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: code, Text: err.Error()})
		return turnDroppedAuthentication
	}
	// Freeze before a turn can park. Delayed execution owns this immutable
	// admission value and never consults temporary controller state.
	prepared := admission.images
	admissionCtx := admission.durableCtx
	if admissionCtx == nil {
		admissionCtx = context.Background()
	}
	run := body
	body = func(ctx context.Context) error {
		return run(contextWithPreparedImageReferences(ctx, prepared))
	}
	if err := c.ensureWriteAuthorityReady(); err != nil {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "input was not accepted: this session is no longer writable — reopen it and try again"})
		return turnDroppedWriteAuthority
	}
	if ledger := c.turnEventLedger(); ledger != nil && ledger.CurrentStatus() == event.TurnRecoveryRequired {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: ErrRecoveryRequired.Error()})
		return turnDroppedWriteAuthority
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return turnDroppedClosed
	}
	if c.rejectDrainingGenerationLocked() {
		c.mu.Unlock()
		c.emitDrainingNotice()
		return turnDroppedDraining
	}
	if c.rotating {
		c.mu.Unlock()
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "input was not accepted: the session is being switched — please resend"})
		return turnDroppedRotating
	}
	if c.turns.phase == session.RuntimeRecoveryRequired {
		c.mu.Unlock()
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: ErrRecoveryRequired.Error()})
		return turnDroppedWriteAuthority
	}
	kind := queuedUser
	if goalRound != nil {
		kind = queuedGoal
	}
	item := queuedTurn{kind: kind, body: body, onStart: onStart, goalRound: goalRound, admissionCtx: admissionCtx}
	switch c.turns.phase {
	case session.RuntimeRunning:
		if parkWhileRunning || c.turns.cancelRequested {
			c.queueTurnLocked(item)
			c.mu.Unlock()
			return turnParked
		}
		c.mu.Unlock()
		return turnDroppedRunning
	case session.RuntimeCancelling:
		c.queueTurnLocked(item)
		c.mu.Unlock()
		return turnParked
	case session.RuntimeFinalizing:
		if !parkWhileFinishing {
			c.mu.Unlock()
			return turnDroppedRunning
		}
		c.queueTurnLocked(item)
		c.mu.Unlock()
		return turnParked
	}
	ctx, cancel, admitted := c.startTurnLocked(context.Background(), item)
	if !admitted {
		c.mu.Unlock()
		c.emitDrainingNotice()
		return turnDroppedDraining
	}
	c.mu.Unlock()
	if onStart != nil {
		onStart()
	}
	c.refreshRuntimeState(event.Event{})
	c.spawnGuardedTurn(ctx, cancel, item)
	return turnStarted
}
