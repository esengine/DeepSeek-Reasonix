package control

import (
	"context"

	"reasonix/internal/event"
	"reasonix/internal/extension"
)

// runSynchronousTurn owns the blocking transport lifecycle. Durable steer
// acknowledgement is intentionally shared with the asynchronous completion
// path, while follow-up dispatch remains owned by the synchronous frontend so
// its response sink stays bound for every queued turn.
func (c *Controller) runSynchronousTurn(
	ctx context.Context,
	onAdmitted func() error,
	run func(context.Context) error,
) error {
	ctx, cancel := context.WithCancel(extension.ContextWithRuntimeOwner(ctx, c.RuntimeOwner()))
	c.mu.Lock()
	// Finishing is part of the gate: TurnDone is still fanning out. Closed
	// seals a torn-down controller. Blocking callers get an error rather than
	// parking because they already own and enforce the request boundary.
	if c.running || c.finishing || c.rotating || c.closed {
		c.mu.Unlock()
		cancel()
		return ErrTurnRunning
	}
	if c.rejectDrainingGenerationLocked() {
		c.mu.Unlock()
		cancel()
		c.emitDrainingNotice()
		return ErrRuntimeDraining
	}
	c.cancel = cancel
	c.running = true
	c.canceling = false
	c.mu.Unlock()
	finish := func() {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.canceling = false
		c.mu.Unlock()
		cancel()
	}
	if onAdmitted != nil {
		if err := onAdmitted(); err != nil {
			finish()
			return err
		}
	}
	defer event.RecordTurnCompletion(c.sink)
	defer func() {
		finish()
		c.onInboxTurnDone()
	}()
	return run(ctx)
}
