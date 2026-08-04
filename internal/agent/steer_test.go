package agent

import (
	"strings"
	"testing"
)

func TestSteerText(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantOK  bool
	}{
		{
			name:    "happy path: prefix + newline + text",
			content: MidTurnSteerPrefix + "\nplease use smaller diffs",
			want:    "please use smaller diffs",
			wantOK:  true,
		},
		{
			name:    "prefix only, no user text",
			content: MidTurnSteerPrefix,
			want:    "",
			wantOK:  true,
		},
		{
			name:    "prefix with trailing whitespace only",
			content: MidTurnSteerPrefix + "\n  ",
			want:    "  ",
			wantOK:  true,
		},
		{
			name:    "round-trip through midTurnSteerMessage",
			content: midTurnSteerMessage("stop using such large diffs"),
			want:    "stop using such large diffs",
			wantOK:  true,
		},
		{
			name:    "user text with leading/trailing spaces preserved (matches live event)",
			content: MidTurnSteerPrefix + "\n   keep going but use read_file first   ",
			want:    "   keep going but use read_file first   ",
			wantOK:  true,
		},
		{
			name:    "regular user message, not steer",
			content: "please use smaller diffs",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "empty string",
			content: "",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "whitespace only",
			content: "   ",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "prefix-like but truncated (no closing bracket)",
			content: "[Mid-turn steer queued by the user. Do not treat this as a new task\nplease go on",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "prefix appears mid-message, not at start",
			content: "hey model " + MidTurnSteerPrefix + "\nuse smaller diffs",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "multiline steer text preserved",
			content: MidTurnSteerPrefix + "\nline one\nline two",
			want:    "line one\nline two",
			wantOK:  true,
		},
		{
			name:    "scheduled task wrapper recognized, label preserved",
			content: MidTurnScheduledMessage("ab12cd34", "check the deploy"),
			want:    "⏰ scheduled task ab12cd34:\ncheck the deploy",
			wantOK:  true,
		},
		{
			name:    "scheduled task wrapper survives delivery-runtime marker",
			content: MidTurnScheduledMessage("ab12cd34", "check the deploy") + "\n\n" + DeliveryRuntimeMarker,
			want:    "⏰ scheduled task ab12cd34:\ncheck the deploy",
			wantOK:  true,
		},
		{
			name:    "multiline scheduled prompt preserved",
			content: MidTurnScheduledMessage("ef01", "line one\nline two"),
			want:    "⏰ scheduled task ef01:\nline one\nline two",
			wantOK:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SteerText(tt.content)
			if ok != tt.wantOK {
				t.Errorf("SteerText() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("SteerText() text = %q, want %q", got, tt.want)
			}
			// Sanity: when ok is true the result must never contain the prefix.
			if ok && strings.Contains(got, MidTurnSteerPrefix) {
				t.Errorf("SteerText() returned text still contains the prefix: %q", got)
			}
		})
	}
}

func TestMidTurnSteerMessageRoundTrip(t *testing.T) {
	inputs := []string{
		"stop",
		"use read_file instead of cat",
		"",
		"  keep going  ",
	}
	for _, in := range inputs {
		msg := midTurnSteerMessage(in)
		got, ok := SteerText(msg)
		if !ok {
			t.Errorf("SteerText(midTurnSteerMessage(%q)): not recognized as steer", in)
			continue
		}
		if got != in {
			t.Errorf("SteerText(midTurnSteerMessage(%q)) = %q, want %q", in, got, in)
		}
	}
}

// TestMidTurnScheduledMessage verifies the consent/anti-spoofing contract of
// scheduled-task injections: the message is explicitly labeled as a scheduled
// task (never as the user), the label+prompt round-trips through SteerText so
// live notices and history replay match, and every synthetic-message site
// (turn counts, titles, previews via IsUserAuthoredTurn) excludes it — a
// poisoned prompt can be seen but can never masquerade as user guidance.
func TestMidTurnScheduledMessage(t *testing.T) {
	const (
		id     = "ab12cd34"
		prompt = "check the deploy"
	)
	msg := MidTurnScheduledMessage(id, prompt)

	if strings.Contains(msg, MidTurnSteerPrefix) {
		t.Errorf("scheduled message must not carry the user-steer prefix:\n%s", msg)
	}
	if !strings.HasPrefix(msg, MidTurnScheduledPrefix) {
		t.Errorf("scheduled message missing its label prefix: %q", msg)
	}

	wantLabel := "⏰ scheduled task " + id + ":\n" + prompt
	got, ok := SteerText(msg)
	if !ok || got != wantLabel {
		t.Errorf("SteerText(scheduled) = %q, %v; want %q, true", got, ok, wantLabel)
	}
	if IsUserAuthoredTurn(msg) {
		t.Error("scheduled injection must not count as a user-authored turn")
	}

	// The id must survive the round trip so a user can cross-reference the
	// notice with /looplist.
	replay, _ := SteerText(msg)
	if !strings.Contains(replay, id) {
		t.Errorf("replay text %q lost the task id", replay)
	}
}

// TestScheduledTaskID: the id parses out of both the wrapped message and its
// SteerText-unwrapped form, and never out of user steers or plain text — so
// the unapplied-rearm hook can only ever re-arm machine-injected fires.
func TestScheduledTaskID(t *testing.T) {
	const id = "ab12cd34"
	wrapped := MidTurnScheduledMessage(id, "check the deploy")
	if got, ok := ScheduledTaskID(wrapped); !ok || got != id {
		t.Errorf("ScheduledTaskID(wrapped) = %q, %v; want %q, true", got, ok, id)
	}
	unwrapped, _ := SteerText(wrapped)
	if got, ok := ScheduledTaskID(unwrapped); !ok || got != id {
		t.Errorf("ScheduledTaskID(unwrapped) = %q, %v; want %q, true", got, ok, id)
	}
	if _, ok := ScheduledTaskID(midTurnSteerMessage("please go on")); ok {
		t.Error("user steer parsed as a scheduled task")
	}
	if _, ok := ScheduledTaskID("please go on"); ok {
		t.Error("plain text parsed as a scheduled task")
	}
}
