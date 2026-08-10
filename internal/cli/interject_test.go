package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestInterjectQueuesWhileRunningWithoutOverwrite(t *testing.T) {
	m := newInboxTestChatTUI(t)
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

	got := m.inboxBodies()
	if want := []string{"first", "second"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("inbox = %v, want %v (second must not overwrite first; empty must not queue)", got, want)
	}
	if m.state != tuiRunning {
		t.Fatalf("queuing input must not change state; got %v", m.state)
	}
}

func TestInterjectLeavesQueueOnTurnDoneForControllerDispatch(t *testing.T) {
	r := &blockingTurnRunner{started: make(chan struct{})}
	dir := t.TempDir()
	ctrl := control.New(control.Options{Runner: r, Sink: event.Discard, SessionDir: dir, Label: "test"})
	ctrl.EnsureSessionPath()
	m := newChatTUI(ctrl, "", make(chan event.Event, 8), 80)
	m.state = tuiRunning
	m.seedInbox("first", "second")

	// CLI no longer dequeues on TurnDone; durable items stay until the controller
	// admits and acks them. Pause so dispatch does not immediately consume.
	_ = m.ctrl.SetInboxPaused(true)
	m0, _ := m.update(agentEventMsg(event.Event{Kind: event.TurnDone}))
	m = m0.(chatTUI)

	got := m.inboxBodies()
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("TurnDone must not drop durable inbox items; got %v", got)
	}
}

func newInboxTestChatTUI(t *testing.T) chatTUI {
	t.Helper()
	dir := t.TempDir()
	ctrl := control.New(control.Options{SessionDir: dir, Label: "test", Sink: event.Discard})
	ctrl.EnsureSessionPath()
	m := newTestChatTUI()
	m.ctrl = ctrl
	return m
}
