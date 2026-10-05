package notify

import (
	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

// Message is the user-visible payload sent to the platform notifier.
type Message struct {
	Title string
	Body  string
}

// Sender delivers a notification without taking ownership of event routing.
type Sender interface {
	Send(Message) error
}

// Sink forwards every event to inner and mirrors configured attention events to
// sender. It holds the setting rather than a copy of it, so a runtime built
// while notifications were off still delivers once they are turned on.
type Sink struct {
	event.AuditForwarder
	inner  event.Sink
	sender Sender
	say    i18n.Messages
	set    *Settings
}

// NewSink wraps an existing event sink with best-effort notification delivery.
func NewSink(inner event.Sink, sender Sender, say i18n.Messages, set *Settings) *Sink {
	return &Sink{AuditForwarder: event.AuditForwarder{Inner: inner}, inner: inner, sender: sender, say: say, set: set}
}

// Emit preserves the underlying event stream before attempting notification side effects.
func (s *Sink) Emit(e event.Event) {
	if s.inner != nil {
		s.inner.Emit(e)
	}
	SendEvent(s.sender, s.say, s.set.Load(), e)
}

// SendEvent applies the same notification rules for paths that do not emit through Sink.
func SendEvent(sender Sender, say i18n.Messages, cfg config.NotificationsConfig, e event.Event) {
	if !cfg.Enabled || sender == nil {
		return
	}
	if msg, ok := message(say, cfg, e); ok {
		_ = sender.Send(msg)
	}
}

// A notification is the one thing this process says outside its own window, so
// it says it in the language the window is set to rather than the kernel's log
// English.
func message(say i18n.Messages, cfg config.NotificationsConfig, e event.Event) (Message, bool) {
	switch e.Kind {
	case event.TurnDone:
		if cfg.TurnDone {
			if e.Err != nil {
				return Message{Title: say.NotifyTitle, Body: say.NotifyTurnFailed}, true
			}
			return Message{Title: say.NotifyTitle, Body: say.NotifyTurnDone}, true
		}
	case event.ApprovalRequest:
		if cfg.ApprovalRequest {
			return Message{Title: say.NotifyTitle, Body: say.NotifyApproval}, true
		}
	case event.AskRequest:
		if cfg.AskRequest {
			return Message{Title: say.NotifyTitle, Body: say.NotifyAsk}, true
		}
	}
	return Message{}, false
}
