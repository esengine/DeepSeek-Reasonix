package cli

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/planmode"
	"reasonix/internal/provider"
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

// TestSubmittedInputsFromHistory proves a resumed session seeds the ↑/↓
// recall list from the user's own prompts: steer and host-reflection messages
// are skipped, compose prefixes are stripped, consecutive duplicates collapse.
func TestSubmittedInputsFromHistory(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "first prompt"},
		{Role: provider.RoleAssistant, Content: "reply"},
		{Role: provider.RoleUser, Content: agent.MidTurnSteerPrefix + "\nadjust the plan"},
		{Role: provider.RoleUser, Content: agent.HostReflectionPrefix + "\nsome guidance"},
		{Role: provider.RoleUser, Content: planmode.Marker + "\n\nsecond prompt"},
		{Role: provider.RoleUser, Content: "second prompt"},
	}
	got := submittedInputsFromHistory(history)
	want := []string{"first prompt", "second prompt"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("submittedInputsFromHistory = %v, want %v", got, want)
	}
}

// TestResumedSessionUpDownRecallsHistory proves a chatTUI built over a
// resumed controller's history lets ↑ recall the previous session's prompt.
func TestResumedSessionUpDownRecallsHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	saveTestSession(t, path, "first prompt")
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	ctrl.Resume(loaded, path)

	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	if len(m.submittedInputs) != 1 || m.submittedInputs[0] != "first prompt" {
		t.Fatalf("resumed history = %v, want [first prompt]", m.submittedInputs)
	}
	m0, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = m0.(chatTUI)
	if got := m.input.Value(); got != "first prompt" {
		t.Fatalf("↑ after resume should recall the past prompt, got %q", got)
	}
}
