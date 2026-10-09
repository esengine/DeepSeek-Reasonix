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

// A recalled entry of several lines does not stop the walk: Up moves through
// its lines, and Up on its first line goes on to the entry before it.
func TestRecallWalksPastAMultiLineEntry(t *testing.T) {
	m, _ := testModel(t)
	typeText(m, "oldest")
	run(m, press(m, "enter"))
	m.composer.SetValue("first\nsecond")
	run(m, press(m, "enter"))
	for i, step := range []struct {
		key  rune
		want string
		line int
	}{
		{tea.KeyUp, "first\nsecond", 1},
		{tea.KeyUp, "first\nsecond", 0},
		{tea.KeyUp, "oldest", 0},
		{tea.KeyDown, "first\nsecond", 1},
		{tea.KeyDown, "", 0},
	} {
		pressCode(m, step.key)
		if got := m.composer.Value(); got != step.want || m.composer.Line() != step.line {
			t.Fatalf("step %d: composer %q on line %d, want %q on line %d", i+1, got, m.composer.Line(), step.want, step.line)
		}
	}
}

// Up on the first line of a draft that spans lines steps into the history, and
// the draft comes back whole at the end of the walk.
func TestRecallStartsFromTheFirstLineOfAMultiLineDraft(t *testing.T) {
	m, _ := testModel(t)
	typeText(m, "old")
	run(m, press(m, "enter"))
	m.composer.SetValue("draft one\ndraft two")
	pressCode(m, tea.KeyUp)
	if got := m.composer.Value(); got != "draft one\ndraft two" {
		t.Fatalf("Up below the first line left the draft as %q", got)
	}
	pressCode(m, tea.KeyUp)
	if got := m.composer.Value(); got != "old" {
		t.Fatalf("Up on the draft's first line recalled %q, want %q", got, "old")
	}
	pressCode(m, tea.KeyDown)
	if got := m.composer.Value(); got != "draft one\ndraft two" {
		t.Fatalf("Down past the newest entry left %q, want the draft back", got)
	}
}
