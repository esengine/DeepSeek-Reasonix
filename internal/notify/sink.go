package notify

import (
	"fmt"
	"strings"
	"time"

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
	event.AuditForwarder
	inner         event.Sink
	sender        Sender
	cfg           config.NotificationsConfig
	turnStartedAt time.Time
}

// NewSink wraps an existing event sink with best-effort notification delivery.
func NewSink(inner event.Sink, sender Sender, cfg config.NotificationsConfig) *Sink {
	return &Sink{AuditForwarder: event.AuditForwarder{Inner: inner}, inner: inner, sender: sender, cfg: cfg}
}

// Emit preserves the underlying event stream before attempting notification side effects.
func (s *Sink) Emit(e event.Event) {
	if s.inner != nil {
		s.inner.Emit(e)
	}
	switch e.Kind {
	case event.TurnStarted:
		s.turnStartedAt = time.Now()
		return
	case event.TurnDone:
		var elapsed time.Duration
		if !s.turnStartedAt.IsZero() {
			elapsed = time.Since(s.turnStartedAt)
			s.turnStartedAt = time.Time{}
		}
		if s.cfg.MinDurationSec > 0 && elapsed < time.Duration(s.cfg.MinDurationSec)*time.Second {
			return
		}
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
			body := "Approval needed"
			if e.Approval.Tool != "" {
				tool := strings.TrimSpace(e.Approval.Tool)
				subject := strings.TrimSpace(e.Approval.Subject)
				if subject != "" {
					body = fmt.Sprintf("Approval needed: %s (%s)", tool, truncateSubject(subject, 40))
				} else {
					body = fmt.Sprintf("Approval needed: %s", tool)
				}
			}
			return Message{Title: "Reasonix", Body: body}, true
		}
	case event.AskRequest:
		if cfg.AskRequest {
			body := "Question needs your answer"
			if len(e.Ask.Questions) > 0 {
				q := e.Ask.Questions[0]
				text := strings.TrimSpace(q.Prompt)
				if text == "" {
					text = strings.TrimSpace(q.Header)
				}
				if text != "" {
					body = fmt.Sprintf("Question: %s", truncateSubject(text, 50))
				}
			}
			return Message{Title: "Reasonix", Body: body}, true
		}
	}
	return Message{}, false
}

func truncateSubject(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
