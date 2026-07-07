package main

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/notify"
)

type desktopRecordSink struct {
	events []event.Kind
}

func (s *desktopRecordSink) Emit(e event.Event) {
	s.events = append(s.events, e.Kind)
}

type desktopRecordSender struct {
	messages []notify.Message
}

func (s *desktopRecordSender) Send(m notify.Message) error {
	s.messages = append(s.messages, m)
	return nil
}

func TestDesktopControllerSinkSendsConfiguredSystemNotifications(t *testing.T) {
	inner := &desktopRecordSink{}
	sender := &desktopRecordSender{}
	app := &App{notificationSender: sender}
	sink := app.desktopControllerSink(inner, config.NotificationsConfig{
		Enabled:         true,
		TurnDone:        true,
		ApprovalRequest: true,
		AskRequest:      true,
	})

	sink.Emit(event.Event{Kind: event.TurnDone})

	if len(inner.events) != 1 || inner.events[0] != event.TurnDone {
		t.Fatalf("forwarded events = %+v, want [TurnDone]", inner.events)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != "Turn finished" {
		t.Fatalf("notification body = %q, want Turn finished", sender.messages[0].Body)
	}
}

func TestDesktopControllerSinkSkipsSystemNotificationsWhenDisabled(t *testing.T) {
	inner := &desktopRecordSink{}
	sender := &desktopRecordSender{}
	app := &App{notificationSender: sender}
	sink := app.desktopControllerSink(inner, config.NotificationsConfig{
		Enabled:  false,
		TurnDone: true,
	})

	sink.Emit(event.Event{Kind: event.TurnDone})

	if len(inner.events) != 1 || inner.events[0] != event.TurnDone {
		t.Fatalf("forwarded events = %+v, want [TurnDone]", inner.events)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("notifications = %d, want 0", len(sender.messages))
	}
}
