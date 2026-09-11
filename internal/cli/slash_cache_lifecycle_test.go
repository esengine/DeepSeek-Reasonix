package cli

import (
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/skill"
)

func TestSlashArgSnapshotSurvivesNoMatchWithinEditingSession(t *testing.T) {
	isolateUserConfig(t)
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.skills = []skill.Skill{{Name: "warm"}}

	m.input.SetValue("/skills show w")
	m.updateCompletion()
	if !m.completion.active || len(m.completion.items) != 1 || m.completion.items[0].label != "warm" {
		t.Fatalf("initial skill completion = %+v, want warm", m.completion)
	}

	// No-match filtering hides the menu, but it does not end the argument
	// editing session. Dynamic completion data must stay frozen until an
	// explicit dismissal, input reset, invalidation, or command change.
	m.skills = []skill.Skill{{Name: "changed"}}
	m.input.SetValue("/skills show z")
	m.updateCompletion()
	if m.completion.active {
		t.Fatalf("no-match completion should be hidden: %+v", m.completion)
	}
	m.input.SetValue("/skills show zz")
	m.updateCompletion()
	m.input.SetValue("/skills show w")
	m.updateCompletion()
	if !m.completion.active || len(m.completion.items) != 1 || m.completion.items[0].label != "warm" {
		t.Fatalf("no-match keystrokes rebuilt argument data: %+v", m.completion)
	}
}

func TestFreeFormSlashSkipsArgDataSnapshot(t *testing.T) {
	isolateUserConfig(t)
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.commands = []command.Command{{Name: "custom", Description: "free-form prompt"}}

	for _, input := range []string{"/custom free text", "/mcp__server__prompt argument"} {
		m.input.SetValue(input)
		m.updateCompletion()
		if m.slashCache != nil && m.slashCache.argBuilt {
			t.Fatalf("free-form slash input %q built dynamic argument data", input)
		}
	}
}
