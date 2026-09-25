package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func pressCode(m *model, code rune) {
	m.Update(tea.KeyPressMsg{Code: code})
}

// Walking back through what was sent and forward again returns the draft
// the walk started from, whatever cursor keys came in between.
func TestRecallReturnsTheDraftItLeft(t *testing.T) {
	m, _ := testModel(t)
	typeText(m, "old")
	run(m, press(m, "enter"))
	typeText(m, "my draft")
	pressCode(m, tea.KeyUp)
	if got := m.composer.Value(); got != "old" {
		t.Fatalf("up recalled %q, want %q", got, "old")
	}
	pressCode(m, tea.KeyRight)
	pressCode(m, tea.KeyHome)
	pressCode(m, tea.KeyDown)
	if got := m.composer.Value(); got != "my draft" {
		t.Fatalf("down past the newest entry left %q, want the draft %q", got, "my draft")
	}
}

// Down on a draft that never left for the history leaves it where it is.
func TestDownOnADraftKeepsIt(t *testing.T) {
	m, _ := testModel(t)
	typeText(m, "old")
	run(m, press(m, "enter"))
	typeText(m, "my draft")
	pressCode(m, tea.KeyDown)
	if got := m.composer.Value(); got != "my draft" {
		t.Fatalf("down turned the draft into %q", got)
	}
}

// Sending a recalled entry starts the next walk from an empty composer, not
// from the draft the previous walk set aside.
func TestSendingARecallDropsTheOldDraft(t *testing.T) {
	m, _ := testModel(t)
	typeText(m, "old")
	run(m, press(m, "enter"))
	typeText(m, "abandoned")
	pressCode(m, tea.KeyUp)
	run(m, press(m, "enter"))
	pressCode(m, tea.KeyUp)
	pressCode(m, tea.KeyDown)
	if got := m.composer.Value(); got != "" {
		t.Fatalf("composer = %q after a sent recall, want empty", got)
	}
}
