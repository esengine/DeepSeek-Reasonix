package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestQueuedFoldedPasteExpandsBeforeInterjectSend(t *testing.T) {
	runner := &recordingTurnRunner{}
	events := make(chan event.Event, 8)
	dir := t.TempDir()
	var ctrl *control.Controller
	ctrl = control.New(control.Options{
		Runner: runner,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				// Seal admission before delivery so the completion tail cannot reopen
				// the durable inbox while this test removes its session directory.
				ctrl.Close()
			}
			events <- e
		}),
		SessionDir: dir,
		Label:      "test",
	})
	defer ctrl.Close()
	ctrl.EnsureSessionPath()
	m := newTestChatTUI()
	m.ctrl = &busyInboxController{SessionAPI: ctrl}
	m.eventCh = make(chan event.Event, 8)
	m.state = tuiRunning
	pasted := strings.Repeat("queued pasted content\n", 10)
	model, _ := m.Update(tea.PasteMsg{Content: pasted})
	m = model.(chatTUI)

	display := strings.TrimSpace(m.input.Value())
	if !strings.Contains(display, "[Pasted text #1") {
		t.Fatalf("paste should be folded, got %q", display)
	}

	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(chatTUI)

	bodies := m.inboxBodies()
	if len(bodies) != 1 {
		t.Fatalf("queue should have 1 item, got %d", len(bodies))
	}
	queued := bodies[0]
	if queued == display {
		t.Fatalf("queued interject kept the folded placeholder: %q", queued)
	}
	for _, want := range []string{
		"queued pasted content",
		"--- Begin [Pasted text #1",
		"--- End [Pasted text #1",
	} {
		if !strings.Contains(queued, want) {
			t.Fatalf("queued interject missing %q in:\n%s", want, queued)
		}
	}

	// Resume inbox so controller can dispatch after TurnDone.
	_ = ctrl.SetInboxPaused(false)
	model, _ = m.Update(agentEventMsg(event.Event{Kind: event.TurnDone}))
	m = model.(chatTUI)
	// Wait for the completed dispatch with admission already sealed.
	waitForCLIEvent(t, events, event.TurnDone)

	if len(runner.inputs) != 1 {
		t.Fatalf("runner should receive queued interject, inputs=%q", runner.inputs)
	}
	sent := runner.inputs[0]
	if sent == display {
		t.Fatalf("runner received the folded placeholder: %q", sent)
	}
	if !strings.Contains(sent, "queued pasted content") {
		t.Fatalf("runner input missing pasted content:\n%s", sent)
	}
}
