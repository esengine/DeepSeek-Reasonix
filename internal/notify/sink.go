package notify

import (
	"reasonix/internal/config"
	"reasonix/internal/event"
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

// Sink forwards every event to inner and mirrors configured attention events to sender.
type Sink struct {
	inner  event.Sink
	sender Sender
	cfg    config.NotificationsConfig
}

// NewSink wraps an existing event sink with best-effort notification delivery.
func NewSink(inner event.Sink, sender Sender, cfg config.NotificationsConfig) *Sink {
	return &Sink{inner: inner, sender: sender, cfg: cfg}
}

// Emit preserves the underlying event stream before attempting notification side effects.
func (s *Sink) Emit(e event.Event) {
	if s.inner != nil {
		s.inner.Emit(e)
	}
	SendEvent(s.sender, s.cfg, e)
}

// SendEvent applies the same notification rules for paths that do not emit through Sink.
func SendEvent(sender Sender, cfg config.NotificationsConfig, e event.Event) {
	if !cfg.Enabled || sender == nil {
		return
	}
	if msg, ok := message(cfg, e); ok {
		_ = sender.Send(msg)
	}
}

func message(cfg config.NotificationsConfig, e event.Event) (Message, bool) {
	switch e.Kind {
	case event.TurnDone:
		if cfg.TurnDone {
			if e.Err != nil {
				return Message{Title: "Reasonix", Body: "Turn failed"}, true
			}
			return Message{Title: "Reasonix", Body: "Turn finished"}, true
		}
	case event.ApprovalRequest:
		if cfg.ApprovalRequest {
			return Message{Title: "Reasonix", Body: "Approval needed"}, true
		}
	case event.AskRequest:
		if cfg.AskRequest {
			return Message{Title: "Reasonix", Body: "Question needs your answer"}, true
		}
	}
	return Message{}, false
}
