package control

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/state/sessioninbox"
)

// RunInboxTurn synchronously claims and executes one durable item. ACP uses
// this path so its blocking response sink remains attached through every
// queued follow-up while Controller still owns durable state and ack semantics.
func (c *Controller) RunInboxTurn(ctx context.Context, id string) error {
	st, err := c.ensureInbox()
	if err != nil {
		return err
	}
	meta, env, err := st.ReadItem(id)
	if err != nil {
		return err
	}
	if meta.State != sessioninbox.StateQueued {
		return sessioninbox.ErrInvalidState
	}
	run, block, err := c.prepareInboxRun(env, meta.Origin)
	if err != nil {
		return err
	}
	if block != "" {
		_ = st.SetState(id, sessioninbox.StateBlocked, block)
		_ = st.SetPaused(true)
		return fmt.Errorf("%w: %s", sessioninbox.ErrInvalidState, block)
	}
	return c.runSynchronousTurn(ctx, func() error {
		c.inbox.admissionMu.Lock()
		defer c.inbox.admissionMu.Unlock()
		c.inbox.trackAdmission(id)
		defer c.inbox.untrackAdmission(id)
		if err := st.ClaimItem(id); err != nil {
			return err
		}
		c.inbox.mu.Lock()
		c.inbox.trackActive(id)
		c.inbox.mu.Unlock()
		return nil
	}, run)
}

// prepareInboxRun materializes one queued item into the turn that runs it.
// origin decides whose turn it is: a host-authored item runs synthetic, so it
// opens no checkpoint, starts no new recovery episode, and is not counted as
// something the user said.
func (c *Controller) prepareInboxRun(env sessioninbox.PromptEnvelope, origin sessioninbox.PromptOrigin) (func(context.Context) error, string, error) {
	submit, frozenImages, block, err := applyInboxReferences(env)
	if err != nil || block != "" {
		return nil, block, err
	}
	display := firstNonEmptyStr(env.DisplayText, submit)
	raw := firstNonEmptyStr(env.RawText, submit)
	requests := controlInvocationsFromInbox(env)
	if len(requests) == 0 {
		return func(ctx context.Context) error {
			return c.runTurnLoop(c.withTurnTags(ctx, turnTags{format: strings.TrimSpace(env.Format), via: env.Via}), orchestratedTurn{
				input: submit, raw: raw, display: display, images: c.frozenTurnImages(frozenImages),
				synthetic: origin.IsHost(),
			})
		}, "", nil
	}
	prepared, err := c.prepareInvocationTurn(submit, requests)
	if err != nil {
		return nil, err.Error(), nil
	}
	return func(ctx context.Context) error {
		return c.runPreparedInvocationTurn(c.withTurnTags(ctx, turnTags{format: strings.TrimSpace(env.Format), via: env.Via}), prepared, submit, raw, display, frozenImages)
	}, "", nil
}

// submitPreparedInboxTurn starts an already-classified inbox envelope without
// interpreting slash commands, shell shortcuts, or @references a second time.
func (c *Controller) submitPreparedInboxTurn(itemID string, run func(context.Context) error) admissionResult {
	return c.runGuardedInbox(run, func() {
		c.inbox.mu.Lock()
		c.inbox.trackActive(itemID)
		c.inbox.mu.Unlock()
	})
}

func sessionInboxInvocations(requests []InvocationRequest) []sessioninbox.StructuredInvocation {
	if len(requests) == 0 {
		return nil
	}
	out := make([]sessioninbox.StructuredInvocation, 0, len(requests))
	for _, request := range requests {
		out = append(out, sessioninbox.StructuredInvocation{Name: request.Name, Kind: request.Kind, Offset: request.Offset})
	}
	return out
}

func controlInvocationsFromInbox(env sessioninbox.PromptEnvelope) []InvocationRequest {
	stored := env.Invocations
	if len(stored) == 0 && env.Invocation != nil {
		stored = []sessioninbox.StructuredInvocation{*env.Invocation}
	}
	out := make([]InvocationRequest, 0, len(stored))
	for _, invocation := range stored {
		out = append(out, InvocationRequest{Name: invocation.Name, Kind: invocation.Kind, Offset: invocation.Offset})
	}
	return out
}
