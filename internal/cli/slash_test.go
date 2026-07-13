package cli

import (
	"testing"

	"reasonix/internal/command"
)

func TestChatCommandNames(t *testing.T) {
	m := chatTUI{commands: []command.Command{{Name: "review"}, {Name: "git:commit"}, {Name: "plan", Hidden: true}}}
	if got := m.commandNames(); got != "/review · /git:commit" {
		t.Errorf("commandNames = %q", got)
	}

	if got := (&chatTUI{}).commandNames(); got != "" {
		t.Errorf("empty commandNames = %q, want \"\"", got)
	}
}
