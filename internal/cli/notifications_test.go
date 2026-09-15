package cli

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/notify"
)

type cliRecordSender struct {
	messages []notify.Message
}

func (s *cliRecordSender) Send(m notify.Message) error {
	s.messages = append(s.messages, m)
	return nil
}

func TestWithNotificationsWrapsCLISinkWithConfiguredSender(t *testing.T) {
	inner := &cliRecordSink{}
	sender := &cliRecordSender{}
	calls := 0
	prev := newNotificationSender
	newNotificationSender = func() notify.Sender {
		calls++
		return sender
	}
	t.Cleanup(func() { newNotificationSender = prev })

	cfg := config.Default()
	cfg.Notifications.Enabled = true

	wrapped := withNotifications(inner, cfg)
	wrapped.Emit(event.Event{Kind: event.TurnDone})

	if calls != 1 {
		t.Fatalf("newNotificationSender calls = %d, want 1", calls)
	}
	if len(inner.events) != 1 || inner.events[0] != event.TurnDone {
		t.Fatalf("forwarded events = %v, want [TurnDone]", inner.events)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != "Turn finished" {
		t.Fatalf("notification body = %q, want Turn finished", sender.messages[0].Body)
	}
}

func TestWithNotificationsIncludesTerminalSenderWhenConfigured(t *testing.T) {
	inner := &cliRecordSink{}
	platformSender := &cliRecordSender{}
	terminalSender := &cliRecordSender{}

	prevPlatform := newNotificationSender
	prevTerminal := newTerminalNotificationSender
	newNotificationSender = func() notify.Sender { return platformSender }
	newTerminalNotificationSender = func() notify.Sender { return terminalSender }
	t.Cleanup(func() {
		newNotificationSender = prevPlatform
		newTerminalNotificationSender = prevTerminal
	})

	cfg := config.Default()
	cfg.Notifications.Enabled = true
	cfg.Notifications.TerminalBell = true

	wrapped := withNotifications(inner, cfg)
	wrapped.Emit(event.Event{Kind: event.TurnDone})

	if len(platformSender.messages) != 1 || len(terminalSender.messages) != 1 {
		t.Fatalf("expected 1 message on each sender, got platform=%d, terminal=%d",
			len(platformSender.messages), len(terminalSender.messages))
	}
}
