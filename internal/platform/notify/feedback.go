package notify

import (
	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
)

// SendFeedbackReply announces a feedback reply under the same two switches as
// every other kind. The text carries no part of the reply.
func SendFeedbackReply(sender Sender, say i18n.Messages, cfg config.NotificationsConfig) {
	if !cfg.Enabled || !cfg.FeedbackReply || sender == nil {
		return
	}
	_ = sender.Send(Message{Title: say.NotifyTitle, Body: say.NotifyFeedbackReply})
}
