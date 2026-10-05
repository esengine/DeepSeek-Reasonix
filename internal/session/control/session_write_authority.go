package control

import (
	"context"
	"errors"
	"reasonix/internal/state/sessionstore"
	"time"

	"reasonix/internal/contract/event"
)

// BindSessionWriteAuthority issues a generation-bound write authority from
// lease and attaches it to the controller's executor session. Controllers must
// call this after acquiring a lease and before entering Ready / Submit /
// autosave / tab publication. A nil lease clears the bound authority so later
// saves fail closed rather than forking recovery under a released lease.
func (c *Controller) BindSessionWriteAuthority(lease *sessionstore.SessionLease) error {
	if c == nil {
		return nil
	}
	gen := sessionstore.NextSessionWriteGeneration()
	if c.executor == nil {
		return nil
	}
	sess := c.executor.Session()
	if sess == nil {
		return nil
	}
	sess.RequireWriteAuthority()
	if lease == nil {
		sess.ClearWriteAuthority()
		return nil
	}
	auth, err := lease.IssueWriteAuthority(gen)
	if err != nil {
		sess.ClearWriteAuthority()
		return err
	}
	// Recovery handoff rebinds the lease before sessionPath updates, so do not
	// require path equality here. Saves still enforce auth.Covers(targetPath).
	sess.BindWriteAuthority(auth)
	return nil
}

func (c *Controller) submitCommandOrTurn(trimmed, input, display string, scopedRefsOnly bool, editedOriginal string, tags turnTags) {
	if err := c.ensureWriteAuthorityReady(); err != nil {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "input was not accepted: this session is no longer writable — reopen it and try again"})
		return
	}
	c.submitCommandOrTurnReady(trimmed, input, display, scopedRefsOnly, editedOriginal, tags)
}

// Run is the synchronous headless turn. It holds the turn gate like every other
// turn, so an inbox turn cannot start on the same executor while it runs, and
// it waits out one already in flight — after a resume the host may dispatch a
// job's interruption notice before the caller's first Run.
func (c *Controller) Run(ctx context.Context, input string) error {
	for {
		if err := c.waitTurnIdle(ctx); err != nil {
			return err
		}
		err := c.runSynchronousTurn(ctx, nil, func(runCtx context.Context) error { return c.runReady(runCtx, input) })
		if !errors.Is(err, ErrTurnRunning) || !c.turnActive() {
			return err
		}
	}
}

// syncTurnPoll is how often Run looks again at a gate another turn holds.
const syncTurnPoll = 20 * time.Millisecond

func (c *Controller) turnActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gate.active()
}

// waitTurnIdle returns once no turn is running or finishing. A closed or
// rotating gate is left to admission to refuse.
func (c *Controller) waitTurnIdle(ctx context.Context) error {
	for c.turnActive() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(syncTurnPoll):
		}
	}
	return nil
}

// ensureWriteAuthorityReady refuses turn admission when the session path is
// set but the bound authority is missing or stale. Empty session paths (no
// persistence yet) are allowed.
func (c *Controller) ensureWriteAuthorityReady() error {
	if c == nil || c.executor == nil {
		return nil
	}
	path := c.SessionPath()
	if path == "" {
		return nil
	}
	sess := c.executor.Session()
	if sess == nil {
		return nil
	}
	auth := sess.WriteAuthority()
	if auth == nil {
		if sess.WriteAuthorityRequired() {
			return sessionstore.ErrSessionWriteAuthorityMissing
		}
		return nil
	}
	if auth.Covers(path) {
		return nil
	}
	return sessionstore.ErrSessionWriteAuthorityStale
}

// authoritySaveError classifies authority failures so recovery does not fire.
func authoritySaveError(err error) bool {
	return errors.Is(err, sessionstore.ErrSessionWriteAuthorityMissing) ||
		errors.Is(err, sessionstore.ErrSessionWriteAuthorityStale)
}
