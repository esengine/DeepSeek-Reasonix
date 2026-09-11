package cli

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestManualNewlineDuringRunningTurnDoesNotSteerOrClearDraft(t *testing.T) {
	dir := t.TempDir()
	ctrl := control.New(control.Options{SessionPath: filepath.Join(dir, "session.jsonl"), SessionDir: dir})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 40)
	m.state = tuiRunning
	m.input.SetValue("first line")

	m0, _ := m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	m = m0.(chatTUI)

	if got := m.input.Value(); got != "first line\n" {
		t.Fatalf("input after running Ctrl+J = %q, want newline-preserved draft", got)
	}
	if items := ctrl.InboxSnapshot().Items; len(items) != 0 {
		t.Fatalf("running Ctrl+J unexpectedly enqueued guidance: %+v", items)
	}
}
