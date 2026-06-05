package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestInterjectQueuesWhileRunningWithoutOverwrite(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning

	m.input.SetValue("first")
	m0, _ := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = m0.(chatTUI)

	m.input.SetValue("second")
	m0, _ = m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = m0.(chatTUI)

	m.input.SetValue("")
	m0, _ = m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = m0.(chatTUI)

	if want := []string{"first", "second"}; len(m.pendingInterject) != len(want) ||
		m.pendingInterject[0] != want[0] || m.pendingInterject[1] != want[1] {
		t.Fatalf("pendingInterject = %v, want %v (second must not overwrite first; empty must not queue)", m.pendingInterject, want)
	}
	if m.state != tuiRunning {
		t.Fatalf("queuing input must not change state; got %v", m.state)
	}
}

func TestInterjectDequeuesFrontOnTurnDone(t *testing.T) {
	r := &blockingTurnRunner{started: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: r, Sink: event.Discard, SessionDir: t.TempDir(), Label: "test"})
	m := newChatTUI(ctrl, "", make(chan event.Event, 8), 80)
	m.state = tuiRunning
	m.pendingInterject = []string{"first", "second"}

	m0, _ := m.update(agentEventMsg(event.Event{Kind: event.TurnDone}))
	m = m0.(chatTUI)

	if len(m.pendingInterject) != 1 || m.pendingInterject[0] != "second" {
		t.Fatalf("TurnDone should dequeue only the front; got %v", m.pendingInterject)
	}
}
