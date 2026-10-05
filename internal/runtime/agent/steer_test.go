package agent

import (
	"reasonix/internal/state/sessionstore"
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
			content: sessionstore.MidTurnSteerPrefix + "\nplease use smaller diffs",
			want:    "please use smaller diffs",
			wantOK:  true,
		},
		{
			name:    "prefix only, no user text",
			content: sessionstore.MidTurnSteerPrefix,
			want:    "",
			wantOK:  true,
		},
		{
			name:    "prefix with trailing whitespace only",
			content: sessionstore.MidTurnSteerPrefix + "\n  ",
			want:    "  ",
			wantOK:  true,
		},
		{
			name:    "round-trip through sessionstore.MidTurnSteerMessage",
			content: sessionstore.MidTurnSteerMessage("stop using such large diffs", false),
			want:    "stop using such large diffs",
			wantOK:  true,
		},
		{
			name:    "user text with leading/trailing spaces preserved (matches live event)",
			content: sessionstore.MidTurnSteerPrefix + "\n   keep going but use read_file first   ",
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
			content: "hey model " + sessionstore.MidTurnSteerPrefix + "\nuse smaller diffs",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "multiline steer text preserved",
			content: sessionstore.MidTurnSteerPrefix + "\nline one\nline two",
			want:    "line one\nline two",
			wantOK:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sessionstore.SteerText(tt.content)
			if ok != tt.wantOK {
				t.Errorf("SteerText() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("SteerText() text = %q, want %q", got, tt.want)
			}
			// Sanity: when ok is true the result must never contain the prefix.
			if ok && strings.Contains(got, sessionstore.MidTurnSteerPrefix) {
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
	for _, host := range []bool{false, true} {
		for _, in := range inputs {
			msg := sessionstore.MidTurnSteerMessage(in, host)
			got, ok := sessionstore.SteerText(msg)
			if !ok {
				t.Errorf("SteerText(sessionstore.MidTurnSteerMessage(%q, %v)): not recognized as steer", in, host)
				continue
			}
			if got != in {
				t.Errorf("SteerText(sessionstore.MidTurnSteerMessage(%q, %v)) = %q, want %q", in, host, got, in)
			}
		}
	}
}

// Recovery guidance rides the steer path but must never present itself as the
// user speaking: a model told the user interrupted answers a person who did not.
func TestHostNoticeDoesNotClaimTheUserSpoke(t *testing.T) {
	msg := sessionstore.MidTurnSteerMessage("a tool failed", true)
	if strings.Contains(msg, sessionstore.MidTurnSteerPrefix) {
		t.Fatalf("host notice = %q, want no user-steer prefix", msg)
	}
	if !strings.HasPrefix(msg, sessionstore.HostNoticePrefix) {
		t.Fatalf("host notice = %q, want the host prefix", msg)
	}
	if !strings.Contains(sessionstore.HostNoticePrefix, "the user did not send this") {
		t.Fatalf("host prefix = %q, want it to disclaim the user", sessionstore.HostNoticePrefix)
	}
}

// Every block the host prepends to a user turn is declared once, and a steer
// behind any of them is still a steer. The recogniser walks that declaration
// rather than a copy: a tag it does not know stops the walk, and the steer
// reads back as the person having typed the host's instructions at themselves.
func TestSteerTextSeesThroughEveryDeclaredTransientBlock(t *testing.T) {
	for _, tag := range sessionstore.TransientUserBlockTags {
		wrapped := "<" + tag + ">\nwhat the host had to say\n</" + tag + ">\n\n" +
			sessionstore.MidTurnSteerPrefix + "\n" + "用户自己说的话"
		got, ok := sessionstore.SteerText(wrapped)
		if !ok || got != "用户自己说的话" {
			t.Errorf("<%s>: SteerText = %q, %v; want the user's own words", tag, got, ok)
		}
	}
}
