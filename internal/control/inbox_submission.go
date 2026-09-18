package control

import (
	"context"
	"errors"
	"maps"
	"reasonix/internal/agent"
	"reasonix/internal/sessioninbox"
	"strings"
)

// EnqueueInbox durably queues an instruction. Only returns a receipt after
// blob+manifest commit. Does not auto-start a turn (call TrySubmit / dispatcher).
func (c *Controller) EnqueueInbox(req InboxRequest) (sessioninbox.InboxReceipt, error) {
	return c.EnqueueInboxContext(c.attachmentContext(), req)
}

func (c *Controller) EnqueueInboxContext(ctx context.Context, req InboxRequest) (sessioninbox.InboxReceipt, error) {
	ctx, cancel := c.NewAttachmentOperationContext(ctx)
	defer cancel()
	c.inbox.prepareMu.Lock()
	defer c.inbox.prepareMu.Unlock()
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	if req.ExpectedSessionPath != "" && st.SessionPath() != req.ExpectedSessionPath {
		return sessioninbox.InboxReceipt{}, ErrInboxSessionChanged
	}
	submit := strings.TrimSpace(firstNonEmptyStr(req.Submit, req.Raw))
	if submit == "" && len(req.Invocations) == 0 {
		submit = strings.TrimSpace(req.Display)
	}
	if submit == "" && len(req.Invocations) == 0 {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrEmpty
	}
	display := firstNonEmptyStr(req.Display, submit)
	raw := firstNonEmptyStr(req.Raw, submit)
	env := sessioninbox.PromptEnvelope{
		DisplayText:  display,
		RawText:      raw,
		SubmitText:   submit,
		Format:       req.Format,
		Source:       req.Source,
		Idempotency:  req.Idempotency,
		ExplicitRefs: append([]string(nil), req.FreezeRefs...),
		Invocations:  sessionInboxInvocations(req.Invocations),
		Extra:        maps.Clone(req.Extra),
	}
	for _, item := range req.Attachments {
		env.AttachmentIdentities = append(env.AttachmentIdentities, item.ClientAttachmentID)
	}
	if len(req.Attachments) > 0 {
		env.FingerprintVersion = 1
		env.RequestFingerprint = inboxAttachmentFingerprint(req, env)
	}
	if receipt, found, err := st.LookupEnvelopeReceipt(req.Idempotency, env); found || err != nil {
		return receipt, err
	}
	if err := c.freezeInboxEnvelopeReferences(ctx, &env, submit, req.FreezeRefs, req.Attachments...); err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	intent := req.Intent
	if intent != sessioninbox.IntentSteer {
		intent = sessioninbox.IntentFollowup
	}
	rec, err := st.Enqueue(sessioninbox.EnqueueRequest{
		Intent:      intent,
		Envelope:    env,
		Source:      req.Source,
		Idempotency: req.Idempotency,
		SessionID:   agent.BranchID(st.SessionPath()),
	})
	if err != nil {
		if errors.Is(err, sessioninbox.ErrCapacityItems) || errors.Is(err, sessioninbox.ErrCapacityBytes) || errors.Is(err, sessioninbox.ErrItemTooLarge) {
			sessioninbox.NoteCapacityReject()
		} else {
			sessioninbox.NoteTxFail()
		}
		return sessioninbox.InboxReceipt{}, err
	}
	if !rec.Idempotent && len(env.ReferenceErrors) > 0 {
		reason := strings.Join(env.ReferenceErrors, "; ")
		if stateErr := st.SetState(rec.ItemID, sessioninbox.StateBlocked, reason); stateErr != nil {
			return sessioninbox.InboxReceipt{}, stateErr
		}
		if pauseErr := st.SetPaused(true); pauseErr != nil {
			return sessioninbox.InboxReceipt{}, pauseErr
		}
		rec.Paused = true
	}
	sessioninbox.NoteEnqueue(int64(len(env.SubmitText)))
	return rec, nil
}
