package control

import (
	"context"
	"errors"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

// BindSessionWriteAuthority issues a generation-bound write authority from
// the lease's SessionWriter onto the executor session. A nil lease clears
// the binding so later saves fail closed instead of forking recovery.
func (c *Controller) BindSessionWriteAuthority(lease *agent.SessionLease) error {
	if c == nil {
		return nil
	}
	gen := agent.NextSessionWriteGeneration()
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
	// Mint through the lease writer so saves serialize and update its baseline.
	// Recovery rebinds the lease before sessionPath updates; saves still
	// enforce auth.Covers(targetPath).
	if err := lease.Writer().Bind(sess, gen); err != nil {
		sess.ClearWriteAuthority()
		return err
	}
	return nil
}

// WriteAuthorityGeneration reports the generation currently bound on this
// controller. Tests use it to prove old generations become stale after rebind.
func (c *Controller) WriteAuthorityGeneration() uint64 {
	if c == nil || c.executor == nil || c.executor.Session() == nil {
		return 0
	}
	return c.executor.Session().WriteAuthority().Generation()
}

func (c *Controller) submitCommandOrTurn(trimmed, input, display string, scopedRefsOnly bool, editedOriginal, format string) {
	if err := c.ensureWriteAuthorityReady(); err != nil {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "input was not accepted: this session is no longer writable — reopen it and try again"})
		return
	}
	c.submitCommandOrTurnReady(trimmed, input, display, scopedRefsOnly, editedOriginal, format)
}

// Run verifies the live write generation before synchronous headless turns.
func (c *Controller) Run(ctx context.Context, input string) error {
	if err := c.ensureWriteAuthorityReady(); err != nil {
		return err
	}
	return c.runReady(ctx, input)
}

// RebindSessionWriteAuthority is a convenience for keepers that already hold a
// lease: it issues a fresh generation so any previous controller authority for
// the same lease object is immediately stale.
func (c *Controller) RebindSessionWriteAuthority(lease *agent.SessionLease) error {
	return c.BindSessionWriteAuthority(lease)
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
			return agent.ErrSessionWriteAuthorityMissing
		}
		return nil
	}
	if auth.Covers(path) {
		return nil
	}
	return agent.ErrSessionWriteAuthorityStale
}

// IssueAndBindWriteAuthority is used by SessionLeaseKeeper and desktop tabs
// after a successful lease acquire/rebind.
func IssueAndBindWriteAuthority(c *Controller, lease *agent.SessionLease) error {
	if c == nil {
		return nil
	}
	return c.BindSessionWriteAuthority(lease)
}

// authoritySaveError classifies authority failures so recovery does not fire.
func authoritySaveError(err error) bool {
	return errors.Is(err, agent.ErrSessionWriteAuthorityMissing) ||
		errors.Is(err, agent.ErrSessionWriteAuthorityStale)
}
