package control

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/session"
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
	if c.managedSessionEvents.Load() {
		if err := c.activateManagedSessionEvents(sess); err != nil {
			sess.ClearWriteAuthority()
			return err
		}
	}
	return nil
}

// activateManagedSessionEvents publishes the replacement runtime's exact
// projection only after the final lease handoff succeeds. This keeps a failed
// settings/model rebuild from changing the still-active controller through the
// shared in-process v3 store.
func (c *Controller) activateManagedSessionEvents(sess *agent.Session) error {
	if c == nil || sess == nil {
		return nil
	}
	if prompt := c.basePrompt(); prompt != "" {
		sess.SetLeadingSystemPromptWithReason(prompt, "managed-runtime-activation")
	}
	// Write-authority binding can run before a service-backed Runtime is
	// published. Its publication path seeds the projection; this preparation
	// path must not manufacture a path-derived sidecar.
	if service, runtime, exclusive := c.v3Binding(); exclusive && service != nil && runtime == nil {
		return nil
	}
	messages := sess.Snapshot()
	snapshot, ok := c.sessionEventSnapshot()
	if !ok || snapshot.EventSequence == 0 {
		if err := c.seedSessionEventsFromExecutor("managed-runtime-activation"); err != nil {
			return err
		}
	} else if !reflect.DeepEqual(snapshot.Projection.ModelMessages, messages) {
		if err := c.replaceSessionEventProjection(context.Background(), "managed-runtime-activation", messages); err != nil {
			return err
		}
	}
	planPayload, _ := json.Marshal(map[string]any{"enabled": c.PlanMode()})
	return c.appendDomainState("plan/state", planPayload, "managed-runtime-activation")
}

// WriteAuthorityGeneration reports the generation currently bound on this
// controller. Tests use it to prove old generations become stale after rebind.
func (c *Controller) WriteAuthorityGeneration() uint64 {
	if c == nil || c.executor == nil || c.executor.Session() == nil {
		return 0
	}
	return c.executor.Session().WriteAuthority().Generation()
}

func (c *Controller) submitCommandOrTurn(trimmed, input, display string, scopedRefsOnly bool, editedOriginal, format string, admission turnAdmission) {
	if err := c.ensureWriteAuthorityReady(); err != nil {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "input was not accepted: this session is no longer writable — reopen it and try again"})
		return
	}
	c.submitCommandOrTurnReady(trimmed, input, display, scopedRefsOnly, editedOriginal, format, admission)
}

// Run verifies the live write generation before synchronous headless turns.
func (c *Controller) Run(ctx context.Context, input string) error {
	prepared, failures := c.prepareSubmissionImagesContext(ctx, SubmissionRequest{Input: input})
	if len(failures) > 0 {
		return ImageReferenceFailures(failures)
	}
	ctx = contextWithPreparedImageReferences(ctx, prepared)
	err := c.runSynchronousTurn(ctx, nil, func(runCtx context.Context) error {
		return c.runReady(runCtx, input)
	})
	if err != nil {
		return err
	}
	return c.waitForGoalTerminal(ctx)
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
	if service, runtime, exclusive := c.v3Binding(); exclusive {
		if runtime == nil {
			if service == nil {
				return session.ErrSessionNotRunning
			}
			if _, err := c.BindFreshSession(context.Background(), ""); err != nil {
				return err
			}
			_, runtime, _ = c.v3Binding()
			if runtime == nil {
				return session.ErrSessionNotRunning
			}
		}
		phase := runtime.StateSnapshot().Phase
		if phase == session.RuntimeRecoveryRequired {
			return session.ErrRecoveryRequired
		}
		if phase == session.RuntimeClosed {
			return session.ErrSessionNotRunning
		}
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
