package control

import (
	"context"

	"reasonix/internal/platform/feedback"
)

// Feedback covers sending the maintainers a report and following what becomes
// of it. The service is install-scoped, not session-scoped: every controller a
// host builds reaches the same identity and receipts on disk.
type Feedback interface {
	FeedbackLimits() feedback.Limits
	FeedbackEnv(surface feedback.Surface, locale string) feedback.Env
	FeedbackDisplayName() string
	SetFeedbackDisplayName(name string) error
	SubmitFeedback(ctx context.Context, d feedback.Draft) (feedback.Receipt, error)
	ListFeedback(ctx context.Context) (feedback.Mine, error)
	ReplyFeedback(ctx context.Context, receipt, body string) (feedback.ReplyReceipt, error)
	MarkFeedbackSeen(receipt string, upTo feedback.ReplyID) error
}

// FeedbackOptions is what assembly hands the controller for Feedback: the
// install's report client, the active provider's kind as a report states it, and
// the frontend a report names (empty leaves /feedback unavailable). A nil
// service answers feedback.ErrDisabled, so a controller built without one
// refuses rather than pretending to send.
type FeedbackOptions struct {
	Service      *feedback.Service
	ProviderKind string
	Surface      feedback.Surface
}

func (c *Controller) FeedbackLimits() feedback.Limits { return feedback.DefaultLimits }

func (c *Controller) FeedbackEnv(surface feedback.Surface, locale string) feedback.Env {
	return feedback.CollectEnv(feedback.EnvContext{Surface: surface, Locale: locale, ProviderKind: c.feedback.ProviderKind})
}

func (c *Controller) FeedbackDisplayName() string {
	if c.feedback.Service == nil {
		return ""
	}
	return c.feedback.Service.DisplayName()
}

func (c *Controller) SetFeedbackDisplayName(name string) error {
	if c.feedback.Service == nil {
		return feedback.ErrDisabled
	}
	return c.feedback.Service.SetDisplayName(name)
}

func (c *Controller) SubmitFeedback(ctx context.Context, d feedback.Draft) (feedback.Receipt, error) {
	if c.feedback.Service == nil {
		return feedback.Receipt{}, feedback.ErrDisabled
	}
	d.Env.ProviderKind = c.feedback.ProviderKind
	return c.feedback.Service.Submit(ctx, d)
}

func (c *Controller) ListFeedback(ctx context.Context) (feedback.Mine, error) {
	if c.feedback.Service == nil {
		return feedback.Mine{}, feedback.ErrDisabled
	}
	return c.feedback.Service.ListMine(ctx)
}

func (c *Controller) ReplyFeedback(ctx context.Context, receipt, body string) (feedback.ReplyReceipt, error) {
	if c.feedback.Service == nil {
		return feedback.ReplyReceipt{}, feedback.ErrDisabled
	}
	return c.feedback.Service.Reply(ctx, receipt, body)
}

func (c *Controller) MarkFeedbackSeen(receipt string, upTo feedback.ReplyID) error {
	if c.feedback.Service == nil {
		return feedback.ErrDisabled
	}
	return c.feedback.Service.MarkSeen(receipt, upTo)
}
