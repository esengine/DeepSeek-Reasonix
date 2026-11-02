package notify

import (
	"errors"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
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
	sink := NewSink(inner, sender, i18n.English, NewSettings(config.NotificationsConfig{
		Enabled:         true,
		TurnDone:        true,
		ApprovalRequest: true,
		AskRequest:      true,
	}))

	sink.Emit(event.Event{Kind: event.ApprovalRequest})
	sink.Emit(event.Event{Kind: event.AskRequest})
	sink.Emit(event.Event{Kind: event.TurnDone})

	if len(inner.events) != 3 {
		t.Fatalf("forwarded events = %d, want 3", len(inner.events))
	}
	if len(sender.messages) != 3 {
		t.Fatalf("notifications = %d, want 3", len(sender.messages))
	}
	if sender.messages[0].Body != i18n.English.NotifyApproval {
		t.Errorf("approval notification body = %q", sender.messages[0].Body)
	}
	if sender.messages[1].Body != i18n.English.NotifyAsk {
		t.Errorf("ask notification body = %q", sender.messages[1].Body)
	}
	if sender.messages[2].Body != i18n.English.NotifyTurnDone {
		t.Errorf("turn notification body = %q", sender.messages[2].Body)
	}
}

func TestSinkSkipsNotificationsWhenDisabled(t *testing.T) {
	inner := &recordSink{}
	sender := &recordSender{}
	sink := NewSink(inner, sender, i18n.English, NewSettings(config.NotificationsConfig{
		Enabled:         false,
		TurnDone:        true,
		ApprovalRequest: true,
		AskRequest:      true,
	}))

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

	SendEvent(sender, i18n.English, config.NotificationsConfig{Enabled: true, TurnDone: true}, event.Event{Kind: event.TurnDone})

	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != i18n.English.NotifyTurnDone {
		t.Errorf("notification body = %q", sender.messages[0].Body)
	}
}

func TestTurnDoneWithErrorSendsFailureNotification(t *testing.T) {
	sender := &recordSender{}

	SendEvent(sender, i18n.English, config.NotificationsConfig{Enabled: true, TurnDone: true}, event.Event{Kind: event.TurnDone, Err: errTestFailure})

	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != i18n.English.NotifyTurnFailed {
		t.Errorf("notification body = %q", sender.messages[0].Body)
	}
}

func TestSinkHonorsPerEventConfig(t *testing.T) {
	sender := &recordSender{}
	sink := NewSink(&recordSink{}, sender, i18n.English, NewSettings(config.NotificationsConfig{
		Enabled:         true,
		TurnDone:        false,
		ApprovalRequest: true,
		AskRequest:      false,
	}))

	sink.Emit(event.Event{Kind: event.TurnDone})
	sink.Emit(event.Event{Kind: event.ApprovalRequest})
	sink.Emit(event.Event{Kind: event.AskRequest})

	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.messages))
	}
	if sender.messages[0].Body != i18n.English.NotifyApproval {
		t.Errorf("notification body = %q", sender.messages[0].Body)
	}
}

// The switch says "notify me", not "notify me after a restart". A runtime built
// while the setting was off is the ordinary case — the window is up before
// anybody opens settings — so it is the one that has to start delivering.
func TestSinkBuiltWhileOffDeliversOnceTurnedOn(t *testing.T) {
	sender := &recordSender{}
	set := NewSettings(config.NotificationsConfig{})
	sink := NewSink(&recordSink{}, sender, i18n.English, set)

	sink.Emit(event.Event{Kind: event.TurnDone})
	if len(sender.messages) != 0 {
		t.Fatalf("notified while off: %+v", sender.messages)
	}

	set.Store(config.NotificationsConfig{Enabled: true, TurnDone: true})
	sink.Emit(event.Event{Kind: event.TurnDone})
	if len(sender.messages) != 1 {
		t.Fatalf("notifications after turning it on = %d, want 1", len(sender.messages))
	}

	set.Store(config.NotificationsConfig{})
	sink.Emit(event.Event{Kind: event.TurnDone})
	if len(sender.messages) != 1 {
		t.Fatalf("kept notifying after it was turned off = %d, want 1", len(sender.messages))
	}
}

// A notification leaves the window, so it is said in the language the window is
// set to rather than the kernel's log English.
func TestNotificationSpeaksTheWindowsLanguage(t *testing.T) {
	sender := &recordSender{}
	SendEvent(sender, i18n.Chinese, config.NotificationsConfig{Enabled: true, TurnDone: true}, event.Event{Kind: event.TurnDone})
	if len(sender.messages) != 1 || sender.messages[0].Body != i18n.Chinese.NotifyTurnDone {
		t.Fatalf("notification body = %+v", sender.messages)
	}
	if sender.messages[0].Body == i18n.English.NotifyTurnDone {
		t.Fatal("notification answered in English for a Chinese window")
	}
}

func TestSendFeedbackReplyNeedsBothSwitches(t *testing.T) {
	for _, c := range []struct {
		cfg  config.NotificationsConfig
		want int
	}{
		{config.NotificationsConfig{FeedbackReply: true}, 0},
		{config.NotificationsConfig{Enabled: true}, 0},
		{config.NotificationsConfig{Enabled: true, FeedbackReply: true}, 1},
	} {
		sender := &recordSender{}
		SendFeedbackReply(sender, i18n.English, c.cfg)
		if len(sender.messages) != c.want {
			t.Errorf("%+v sent %d, want %d", c.cfg, len(sender.messages), c.want)
		}
	}
	sender := &recordSender{}
	SendFeedbackReply(sender, i18n.English, config.NotificationsConfig{Enabled: true, FeedbackReply: true})
	if sender.messages[0].Body != i18n.English.NotifyFeedbackReply {
		t.Errorf("body = %q", sender.messages[0].Body)
	}
}

type chimingSender struct {
	recordSender
	chimes int
}

func (s *chimingSender) Chime() error {
	s.chimes++
	return nil
}

func TestSoundIsItsOwnSwitchAndFollowsTheEventSwitches(t *testing.T) {
	cases := []struct {
		name         string
		cfg          config.NotificationsConfig
		wantMessages int
		wantChimes   int
	}{
		{"off by default", config.NotificationsConfig{TurnDone: true, ApprovalRequest: true, AskRequest: true}, 0, 0},
		{"sound without system notifications", config.NotificationsConfig{Sound: true, TurnDone: true}, 0, 1},
		{"both", config.NotificationsConfig{Enabled: true, Sound: true, TurnDone: true}, 1, 1},
		{"notifications without sound", config.NotificationsConfig{Enabled: true, TurnDone: true}, 1, 0},
		{"sound but the event switch is off", config.NotificationsConfig{Sound: true}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &chimingSender{}
			sink := NewSink(&recordSink{}, sender, i18n.English, NewSettings(tc.cfg))
			sink.Emit(event.Event{Kind: event.TurnDone})
			if len(sender.messages) != tc.wantMessages || sender.chimes != tc.wantChimes {
				t.Fatalf("messages=%d chimes=%d, want %d and %d", len(sender.messages), sender.chimes, tc.wantMessages, tc.wantChimes)
			}
		})
	}
}

func TestFeedbackReplyChimesUnderTheSameSwitches(t *testing.T) {
	sender := &chimingSender{}
	SendFeedbackReply(sender, i18n.English, config.NotificationsConfig{Sound: true, FeedbackReply: true})
	if sender.chimes != 1 || len(sender.messages) != 0 {
		t.Fatalf("chimes=%d messages=%d", sender.chimes, len(sender.messages))
	}
	SendFeedbackReply(sender, i18n.English, config.NotificationsConfig{Sound: true})
	if sender.chimes != 1 {
		t.Fatal("a reply chimed with its own switch off")
	}
}

// A person taking a question over is not a new question: no second notification.
func TestSinkDoesNotNotifyAgainWhenAQuestionIsTakenOver(t *testing.T) {
	sender := &recordSender{}
	sink := NewSink(&recordSink{}, sender, i18n.English, NewSettings(config.NotificationsConfig{Enabled: true, AskRequest: true}))
	sink.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: "a1"}})
	sink.Emit(event.Event{Kind: event.AskEngaged, Ask: event.Ask{ID: "a1"}})
	if len(sender.messages) != 1 {
		t.Fatalf("notifications = %d, want 1: taking the question over must not announce it again", len(sender.messages))
	}
}
