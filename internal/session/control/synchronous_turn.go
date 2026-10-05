package control

import (
	"context"

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/extension"
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
	if err := c.ensureWriteAuthorityReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(extension.ContextWithRuntimeOwner(ctx, c.RuntimeOwner()))
	c.mu.Lock()
	// Finishing is part of the gate: TurnDone is still fanning out. Closed
	// seals a torn-down controller. Blocking callers get an error rather than
	// parking because they already own and enforce the request boundary.
	if c.gate.busy() {
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
	c.gate.begin(cancel)
	c.mu.Unlock()
	finish := func() {
		c.mu.Lock()
		c.gate.end()
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
