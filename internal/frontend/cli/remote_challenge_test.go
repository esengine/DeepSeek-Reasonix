package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/platform/remote"
)

func TestTerminalChallengePreservesTextWithoutTerminalControls(t *testing.T) {
	var out bytes.Buffer
	question := renderTerminalChallenge(&out, remote.Challenge{
		Host:        "trusted-host",
		Name:        "\x1b[2J\x1b[HServer title\rreplacement\b",
		Instruction: "Exact instruction\nSecond line\x1b]0;spoofed-title\a\u202e",
		Question:    "\x1b[2K\rUnclassified question:\x1b[1A",
	})
	if got, want := out.String(), "trusted-host\nServer titlereplacement\nExact instruction\nSecond line\n"; got != want {
		t.Fatalf("display=%q, want %q", got, want)
	}
	if question != "Unclassified question:" {
		t.Fatalf("question=%q", question)
	}
	for _, control := range []string{"\x1b", "\r", "\b", "\a", "\u202e"} {
		if strings.Contains(out.String()+question, control) {
			t.Fatalf("terminal control remains: %q", control)
		}
	}
}

func TestTerminalChallengeCancellationBeforeInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := terminalChallengePrompt(ctx, remote.Challenge{AnswerRequired: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}
