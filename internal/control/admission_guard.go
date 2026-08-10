package control

import (
	"context"

	"reasonix/internal/event"
	"reasonix/internal/extension"
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
)

// runGuarded runs body under a fresh context, guarding concurrent turns.
// Finishing-window arrivals park instead of dropping (see admissionResult).
func (c *Controller) runGuarded(body func(ctx context.Context) error) admissionResult {
	return c.admitGuardedTurn(body, false, true, nil)
}

// runGuardedOrPark admits like runGuarded but parks the body while another
// turn is running instead of using the deliberately-silent running drop.
// Reserved for inputs that are the user's own words (the steer fallback):
// the FIFO drain in finishGuardedTurn delivers them the moment the current
// turn finishes.
func (c *Controller) runGuardedOrPark(body func(ctx context.Context) error) admissionResult {
	return c.admitGuardedTurn(body, true, true, nil)
}

// runGuardedInbox admits a durable item without parking it in volatile memory.
// onStart runs after admission is reserved and before its goroutine can finish.
func (c *Controller) runGuardedInbox(body func(ctx context.Context) error, onStart func()) admissionResult {
	return c.admitGuardedTurn(body, false, false, onStart)
}

func (c *Controller) admitGuardedTurn(body func(ctx context.Context) error, parkWhileRunning, parkWhileFinishing bool, onStart func()) admissionResult {
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
	if c.running {
		if parkWhileRunning {
			c.parkedTurns = append(c.parkedTurns, body)
			c.mu.Unlock()
			return turnParked
		}
		c.mu.Unlock()
		return turnDroppedRunning
	}
	if c.finishing {
		if !parkWhileFinishing {
			c.mu.Unlock()
			return turnDroppedRunning
		}
		c.parkedTurns = append(c.parkedTurns, body)
		c.mu.Unlock()
		return turnParked
	}
	ctx, cancel := context.WithCancel(extension.ContextWithRuntimeOwner(context.Background(), c.runtimeOwner))
	c.cancel = cancel
	c.running = true
	c.canceling = false
	c.mu.Unlock()
	if onStart != nil {
		onStart()
	}
	c.spawnGuardedTurn(ctx, cancel, body)
	return turnStarted
}
