package notify

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

// TestMessage covers the kind+flag → (Message, bool) mapping used by SendEvent.
func TestMessage(t *testing.T) {
	allOff := config.NotificationsConfig{}

	cases := []struct {
		name string
		cfg  config.NotificationsConfig
		ev   event.Event
		want Message
		ok   bool
	}{
		{
			name: "turn done enabled no error",
			cfg:  config.NotificationsConfig{TurnDone: true},
			ev:   event.Event{Kind: event.TurnDone},
			want: Message{Title: "Reasonix", Body: "Turn finished"},
			ok:   true,
		},
		{
			name: "turn done enabled with error",
			cfg:  config.NotificationsConfig{TurnDone: true},
			ev:   event.Event{Kind: event.TurnDone, Err: errTestFailure},
			want: Message{Title: "Reasonix", Body: "Turn failed"},
			ok:   true,
		},
		{
			name: "turn done disabled",
			cfg:  allOff,
			ev:   event.Event{Kind: event.TurnDone},
			ok:   false,
		},
		{
			name: "approval request enabled",
			cfg:  config.NotificationsConfig{ApprovalRequest: true},
			ev:   event.Event{Kind: event.ApprovalRequest},
			want: Message{Title: "Reasonix", Body: "Approval needed"},
			ok:   true,
		},
		{
			name: "approval request disabled",
			cfg:  allOff,
			ev:   event.Event{Kind: event.ApprovalRequest},
			ok:   false,
		},
		{
			name: "ask request enabled",
			cfg:  config.NotificationsConfig{AskRequest: true},
			ev:   event.Event{Kind: event.AskRequest},
			want: Message{Title: "Reasonix", Body: "Question needs your answer"},
			ok:   true,
		},
		{
			name: "ask request disabled",
			cfg:  allOff,
			ev:   event.Event{Kind: event.AskRequest},
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := message(tc.cfg, tc.ev)
			if ok != tc.ok {
				t.Fatalf("message() ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if msg != tc.want {
				t.Errorf("message() = %+v, want %+v", msg, tc.want)
			}
		})
	}
}

// TestMessageIgnoresOtherKinds: every kind that is not TurnDone, ApprovalRequest
// or AskRequest maps to false even when all notification flags are on.
func TestMessageIgnoresOtherKinds(t *testing.T) {
	allOn := config.NotificationsConfig{TurnDone: true, ApprovalRequest: true, AskRequest: true}
	otherKinds := []event.Kind{
		event.TurnStarted,
		event.Reasoning,
		event.Text,
		event.Message,
		event.ToolDispatch,
		event.ToolResult,
		event.Usage,
		event.Notice,
		event.Phase,
		event.CompactionStarted,
		event.CompactionDone,
		event.ToolProgress,
		event.Retrying,
		event.Steer,
	}

	for _, k := range otherKinds {
		if msg, ok := message(allOn, event.Event{Kind: k}); ok {
			t.Errorf("kind %v produced a notification (%+v), want none", k, msg)
		}
	}
}

// TestMessageUnknownKindValue guards against accidental notification of an
// out-of-range Kind value (e.g. a future kind added before config wiring).
func TestMessageUnknownKindValue(t *testing.T) {
	allOn := config.NotificationsConfig{TurnDone: true, ApprovalRequest: true, AskRequest: true}
	if msg, ok := message(allOn, event.Event{Kind: event.Kind(999)}); ok {
		t.Errorf("unknown kind produced a notification (%+v), want none", msg)
	}
}
