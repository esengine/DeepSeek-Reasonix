package notify

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

var errTestFailure = errors.New("failed")

type recordSink struct {
	events   []event.Kind
	recovery []event.ProtocolRecoveryAudit
}

func (s *recordSink) Emit(e event.Event) {
	s.events = append(s.events, e.Kind)
}

func (s *recordSink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	s.recovery = append(s.recovery, a)
}

type recordSender struct {
	messages []Message
}

func TestSinkForwardsProtocolRecoveryWithoutNotification(t *testing.T) {
	inner := &recordSink{}
	sender := &recordSender{}
	sink := NewSink(inner, sender, config.NotificationsConfig{Enabled: true, TurnDone: true})

	event.RecordProtocolRecovery(sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})

	if len(inner.recovery) != 1 || inner.recovery[0].Kind != event.ProtocolRecoveryMissingReasoningFallback {
		t.Fatalf("forwarded protocol recovery = %+v", inner.recovery)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("protocol recovery sent user notification: %+v", sender.messages)
	}
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

func TestSinkMinDurationSuppressesShortTurn(t *testing.T) {
	sender := &recordSender{}
	sink := NewSink(&recordSink{}, sender, config.NotificationsConfig{
		Enabled:        true,
		TurnDone:       true,
		MinDurationSec: 5,
	})

	sink.Emit(event.Event{Kind: event.TurnStarted})
	// Turn finishes almost immediately (< 5s threshold)
	sink.Emit(event.Event{Kind: event.TurnDone})

	if len(sender.messages) != 0 {
		t.Fatalf("short turn produced %d notifications, want 0", len(sender.messages))
	}
}

func TestSinkMinDurationAllowsLongTurn(t *testing.T) {
	sender := &recordSender{}
	sink := NewSink(&recordSink{}, sender, config.NotificationsConfig{
		Enabled:        true,
		TurnDone:       true,
		MinDurationSec: 1,
	})

	sink.Emit(event.Event{Kind: event.TurnStarted})
	// Simulate start in the past
	sink.turnStartedAt = time.Now().Add(-2 * time.Second)
	sink.Emit(event.Event{Kind: event.TurnDone})

	if len(sender.messages) != 1 {
		t.Fatalf("long turn produced %d notifications, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != "Turn finished" {
		t.Errorf("notification body = %q, want 'Turn finished'", sender.messages[0].Body)
	}
}

func TestSinkApprovalContextFormatting(t *testing.T) {
	sender := &recordSender{}
	sink := NewSink(&recordSink{}, sender, config.NotificationsConfig{
		Enabled:         true,
		ApprovalRequest: true,
	})

	sink.Emit(event.Event{
		Kind: event.ApprovalRequest,
		Approval: event.Approval{
			Tool:    "bash",
			Subject: "rm -rf dist/",
		},
	})

	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	want := "Approval needed: bash (rm -rf dist/)"
	if sender.messages[0].Body != want {
		t.Errorf("approval notification body = %q, want %q", sender.messages[0].Body, want)
	}
}

func TestSinkAskContextFormatting(t *testing.T) {
	sender := &recordSender{}
	sink := NewSink(&recordSink{}, sender, config.NotificationsConfig{
		Enabled:    true,
		AskRequest: true,
	})

	sink.Emit(event.Event{
		Kind: event.AskRequest,
		Ask: event.Ask{
			Questions: []event.AskQuestion{
				{Prompt: "Which database migration strategy should we use?"},
			},
		},
	})

	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	want := "Question: Which database migration strategy should we use?"
	if sender.messages[0].Body != want {
		t.Errorf("ask notification body = %q, want %q", sender.messages[0].Body, want)
	}
}

func TestTerminalSenderEscapeAndBell(t *testing.T) {
	var buf bytes.Buffer
	ts := &TerminalSender{Out: &buf}

	err := ts.Send(Message{Title: "Reasonix", Body: "Turn finished"})
	if err != nil {
		t.Fatalf("TerminalSender.Send error: %v", err)
	}

	out := buf.String()
	// Must contain ASCII bell
	if !strings.Contains(out, "\a") {
		t.Errorf("output missing bell character: %q", out)
	}
	// Must contain OSC 777
	if !strings.Contains(out, "\x1b]777;notify;Reasonix;Turn finished\x1b\\") {
		t.Errorf("output missing OSC 777 sequence: %q", out)
	}
	// Must contain OSC 9
	if !strings.Contains(out, "\x1b]9;Turn finished\x1b\\") {
		t.Errorf("output missing OSC 9 sequence: %q", out)
	}
}

func TestMultiSenderDelivery(t *testing.T) {
	s1 := &recordSender{}
	s2 := &recordSender{}
	multi := MultiSender{s1, s2}

	msg := Message{Title: "Test", Body: "Hello"}
	if err := multi.Send(msg); err != nil {
		t.Fatalf("MultiSender.Send error: %v", err)
	}

	if len(s1.messages) != 1 || len(s2.messages) != 1 {
		t.Fatalf("expected 1 message each, got s1=%d, s2=%d", len(s1.messages), len(s2.messages))
	}
}
