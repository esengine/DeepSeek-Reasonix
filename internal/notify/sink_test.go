package notify

import (
	"errors"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

var errTestFailure = errors.New("failed")

type recordSink struct {
	events []event.Kind
}

func (s *recordSink) Emit(e event.Event) {
	s.events = append(s.events, e.Kind)
}

type recordSender struct {
	messages []Message
}

func (s *recordSender) Send(m Message) error {
	s.messages = append(s.messages, m)
	return nil
}

func TestSinkForwardsEventsAndSendsConfiguredNotifications(t *testing.T) {
	inner := &recordSink{}
	sender := &recordSender{}
	sink := NewSink(inner, sender, config.NotificationsConfig{
		Enabled:         true,
		TurnDone:        true,
		ApprovalRequest: true,
		AskRequest:      true,
	})

	sink.Emit(event.Event{Kind: event.ApprovalRequest})
	sink.Emit(event.Event{Kind: event.AskRequest})
	sink.Emit(event.Event{Kind: event.TurnDone})

	if len(inner.events) != 3 {
		t.Fatalf("forwarded events = %d, want 3", len(inner.events))
	}
	if len(sender.messages) != 3 {
		t.Fatalf("notifications = %d, want 3", len(sender.messages))
	}
	if sender.messages[0].Body != "Approval needed" {
		t.Errorf("approval notification body = %q", sender.messages[0].Body)
	}
	if sender.messages[1].Body != "Question needs your answer" {
		t.Errorf("ask notification body = %q", sender.messages[1].Body)
	}
	if sender.messages[2].Body != "Turn finished" {
		t.Errorf("turn notification body = %q", sender.messages[2].Body)
	}
}

func TestSinkSkipsNotificationsWhenDisabled(t *testing.T) {
	inner := &recordSink{}
	sender := &recordSender{}
	sink := NewSink(inner, sender, config.NotificationsConfig{
		Enabled:         false,
		TurnDone:        true,
		ApprovalRequest: true,
		AskRequest:      true,
	})

	sink.Emit(event.Event{Kind: event.TurnDone})

	if len(inner.events) != 1 {
		t.Fatalf("forwarded events = %d, want 1", len(inner.events))
	}
	if len(sender.messages) != 0 {
		t.Fatalf("notifications = %d, want 0", len(sender.messages))
	}
}

func TestSendEventUsesSameNotificationRules(t *testing.T) {
	sender := &recordSender{}

	SendEvent(sender, config.NotificationsConfig{Enabled: true, TurnDone: true}, event.Event{Kind: event.TurnDone})

	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != "Turn finished" {
		t.Errorf("notification body = %q", sender.messages[0].Body)
	}
}

func TestTurnDoneWithErrorSendsFailureNotification(t *testing.T) {
	sender := &recordSender{}

	SendEvent(sender, config.NotificationsConfig{Enabled: true, TurnDone: true}, event.Event{Kind: event.TurnDone, Err: errTestFailure})

	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != "Turn failed" {
		t.Errorf("notification body = %q", sender.messages[0].Body)
	}
}

func TestSinkHonorsPerEventConfig(t *testing.T) {
	sender := &recordSender{}
	sink := NewSink(&recordSink{}, sender, config.NotificationsConfig{
		Enabled:         true,
		TurnDone:        false,
		ApprovalRequest: true,
		AskRequest:      false,
	})

	sink.Emit(event.Event{Kind: event.TurnDone})
	sink.Emit(event.Event{Kind: event.ApprovalRequest})
	sink.Emit(event.Event{Kind: event.AskRequest})

	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != "Approval needed" {
		t.Errorf("notification body = %q", sender.messages[0].Body)
	}
}
