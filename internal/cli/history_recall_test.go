package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestUpDownRecallsHistoryWhileRunning proves ↑/↓ recall the submitted-input
// history while a turn is running (when there is no queued-message queue to
// browse), matching the idle behaviour.
func TestUpDownRecallsHistoryWhileRunning(t *testing.T) {
	m := newTestChatTUI()
	m.submittedInputs = []string{"first prompt", "second prompt"}
	m.state = tuiRunning
	m.input.SetValue("draft")

	// ↑ with an empty queue recalls the latest submitted prompt.
	m0, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = m0.(chatTUI)
	if got := m.input.Value(); got != "second prompt" {
		t.Fatalf("↑ while running should recall history, got %q", got)
	}
	// ↑ again walks older.
	m0, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = m0.(chatTUI)
	if got := m.input.Value(); got != "first prompt" {
		t.Fatalf("second ↑ should walk older, got %q", got)
	}
	// ↓ walks back to the draft.
	m0, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = m0.(chatTUI)
	if got := m.input.Value(); got != "second prompt" {
		t.Fatalf("↓ should walk newer, got %q", got)
	}
	m0, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = m0.(chatTUI)
	if got := m.input.Value(); got != "draft" {
		t.Fatalf("↓ past the newest should restore the draft, got %q", got)
	}
}

// TestUpDownWhileRunningPrefersQueue proves a non-empty interject queue keeps
// ↑/↓ browsing the queue, not the history.
func TestUpDownWhileRunningPrefersQueue(t *testing.T) {
	m := newTestChatTUI()
	m.submittedInputs = []string{"old prompt"}
	m.pendingInterject = []string{"queued message"}
	m.state = tuiRunning
	m.input.SetValue("draft")

	m0, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = m0.(chatTUI)
	if got := m.input.Value(); got != "queued message" {
		t.Fatalf("↑ with a queue should browse the queue, got %q", got)
	}
}

// TestUpDownRecallsHistoryWhileIdle keeps the idle path pinned: ↑/↓ recall
// history when no turn is running.
func TestUpDownRecallsHistoryWhileIdle(t *testing.T) {
	m := newTestChatTUI()
	m.submittedInputs = []string{"first prompt"}
	m.state = tuiIdle

	m0, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = m0.(chatTUI)
	if got := m.input.Value(); got != "first prompt" {
		t.Fatalf("↑ while idle should recall history, got %q", got)
	}
}
