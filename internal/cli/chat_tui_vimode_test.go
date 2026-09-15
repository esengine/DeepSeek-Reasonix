package cli

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// newViTestTUI builds an idle chatTUI with ui.commandmode = "vi".
func newViTestTUI(t *testing.T) chatTUI {
	t.Helper()
	cfg := config.Default()
	cfg.UI.CommandMode = "vi"
	m := newChatTUI(control.New(control.Options{}), "", make(chan event.Event, 1), 80)
	m.cfg = cfg
	out, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return out.(chatTUI)
}

func viKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestViEscEntersCommandModeAndInsertLeavesIt(t *testing.T) {
	m := newViTestTUI(t)
	if m.viInCommand() {
		t.Fatal("should start in insert mode")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = out.(chatTUI)
	if !m.viInCommand() {
		t.Fatal("esc should enter command mode")
	}
	// A command-mode key (i) returns to insert editing.
	out, _ = m.Update(viKey('i'))
	m = out.(chatTUI)
	if m.viInCommand() {
		t.Fatal("i should leave command mode")
	}
}

func TestViEscLeavingInsertStepsCaretLeft(t *testing.T) {
	esc := tea.KeyPressMsg{Code: tea.KeyEsc}
	m := newViTestTUI(t)
	m.input.SetValue("abc")
	m.input.SetCursorColumn(3) // after the last char, as after typing "abc"
	out, _ := m.Update(esc)
	m = out.(chatTUI)
	if !m.viInCommand() {
		t.Fatal("esc should enter command mode")
	}
	if got := m.input.Column(); got != 2 {
		t.Fatalf("caret after esc from insert = %d, want 2 (one rune left of end)", got)
	}
	// A second Esc while already in command mode must not move the caret.
	out, _ = m.Update(esc)
	m = out.(chatTUI)
	if !m.viInCommand() {
		t.Fatal("esc must keep command mode")
	}
	if got := m.input.Column(); got != 2 {
		t.Fatalf("caret after second esc = %d, want 2 (no-op)", got)
	}
}

func TestViDeleteCharX(t *testing.T) {
	m := newViTestTUI(t)
	m.input.SetValue("abc")
	m.input.SetCursorColumn(0)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = out.(chatTUI)
	if !m.viInCommand() {
		t.Fatal("esc should enter command mode")
	}
	out, _ = m.Update(viKey('x'))
	m = out.(chatTUI)
	if got := m.input.Value(); got != "bc" {
		t.Fatalf("x at col 0 = %q, want bc", got)
	}
	if !m.viInCommand() {
		t.Fatal("x must stay in command mode")
	}
	// Pressing an unrecognized command-mode key must not insert text.
	out, _ = m.Update(viKey('q'))
	m = out.(chatTUI)
	if got := m.input.Value(); got != "bc" {
		t.Fatalf("command mode ignored 'q' but value = %q, want bc", got)
	}
	if !m.viInCommand() {
		t.Fatal("must remain in command mode after ignored key")
	}
}

func TestViDeleteCharAtEndOfLineNoop(t *testing.T) {
	m := newViTestTUI(t)
	m.input.SetValue("abc")
	m.input.SetCursorColumn(3) // caret past last char
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = out.(chatTUI)
	// Esc from insert steps left; move back to the true end in command mode.
	out, _ = m.Update(viKey('$'))
	m = out.(chatTUI)
	if got := m.input.Column(); got != 3 {
		t.Fatalf("$ caret col = %d, want 3", got)
	}
	out, _ = m.Update(viKey('x'))
	m = out.(chatTUI)
	if got := m.input.Value(); got != "abc" {
		t.Fatalf("x at end-of-line = %q, want abc unchanged", got)
	}
}

func TestViInsertAfterLastChar(t *testing.T) {
	m := newViTestTUI(t)
	m.input.SetValue("abc")
	m.input.SetCursorColumn(1) // caret after 'a'
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = out.(chatTUI)
	out, _ = m.Update(viKey('A')) // append after last char
	m = out.(chatTUI)
	if m.viInCommand() {
		t.Fatal("A should enter insert mode")
	}
	if got := m.input.Column(); got != 3 {
		t.Fatalf("A caret col = %d, want 3 (end of 'abc')", got)
	}
	out, _ = m.Update(viKey('d'))
	m = out.(chatTUI)
	if got := m.input.Value(); got != "abcd" {
		t.Fatalf("typing after A = %q, want abcd", got)
	}
}

func TestViInsertBeforeFirstChar(t *testing.T) {
	m := newViTestTUI(t)
	m.input.SetValue("bc")
	m.input.SetCursorColumn(2) // caret at end
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = out.(chatTUI)
	out, _ = m.Update(viKey('I')) // insert before first char
	m = out.(chatTUI)
	if m.viInCommand() {
		t.Fatal("I should enter insert mode")
	}
	out, _ = m.Update(viKey('a'))
	m = out.(chatTUI)
	if got := m.input.Value(); got != "abc" {
		t.Fatalf("typing after I = %q, want abc", got)
	}
}

func TestViCtrlDQuitOnlyInInsertModeEmptyPrompt(t *testing.T) {
	ctrlD := tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}

	m := newViTestTUI(t)
	// Insert mode, truly empty prompt: quits.
	m.input.SetValue("")
	if _, cmd := m.Update(ctrlD); cmd == nil {
		t.Fatal("^D in insert mode on empty prompt should request shutdown")
	}

	// Insert mode but with content (even a single space): no quit.
	m = newViTestTUI(t)
	m.input.SetValue(" ")
	if _, cmd := m.Update(ctrlD); cmd != nil {
		t.Fatal("^D with a space must not quit in vi mode")
	}

	// Command mode on the empty prompt: no quit.
	m = newViTestTUI(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = out.(chatTUI)
	if _, cmd := m.Update(ctrlD); cmd != nil {
		t.Fatal("^D in command mode must not quit")
	}
}

func TestViCtrlCLeavesCommandModeButNeverQuits(t *testing.T) {
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	m := newViTestTUI(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = out.(chatTUI)
	if !m.viInCommand() {
		t.Fatal("setup: expected command mode")
	}
	out, _ = m.Update(ctrlC)
	m = out.(chatTUI)
	if m.viInCommand() {
		t.Fatal("^C should leave command mode back into insert")
	}
	if m.state != tuiIdle {
		t.Fatalf("^C on idle must not quit; state = %v", m.state)
	}
}

// TestViCtrlCSavesDraftToHistoryAndClears: pressing ^C at the prompt with typed
// text must save the draft to the cmdline history verbatim and clear the prompt,
// so an accidental interrupt never loses it.
func TestViCtrlCSavesDraftToHistoryAndClears(t *testing.T) {
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	m := newViTestTUI(t)
	m.input.SetValue("half-written draft")

	out, _ := m.Update(ctrlC)
	m = out.(chatTUI)

	if got := m.input.Value(); got != "" {
		t.Fatalf("prompt after ^C = %q, want empty", got)
	}
	if len(m.submittedInputs) == 0 || m.submittedInputs[len(m.submittedInputs)-1] != "half-written draft" {
		t.Fatalf("draft not saved to cmdline history: %v", m.submittedInputs)
	}
	if m.state != tuiIdle {
		t.Fatalf("^C on idle must not quit; state = %v", m.state)
	}
}

func TestViAskChooserIgnoresEsc(t *testing.T) {
	ask := event.Ask{
		ID: "ask-1",
		Questions: []event.AskQuestion{{
			ID:     "q1",
			Prompt: "Pick one",
			Options: []event.AskOption{
				{Label: "Option A"},
				{Label: "Option B"},
			},
		}},
	}
	esc := tea.KeyPressMsg{Code: tea.KeyEsc}

	// vi mode: Esc must not dismiss the ask card.
	m := newViTestTUI(t)
	m.chooser = newChooser(ask)
	if !m.viActive() {
		t.Fatal("setup: expected vi active")
	}
	out, _ := m.handleChooserKey(esc)
	m = out.(chatTUI)
	if m.chooser == nil {
		t.Fatal("vi mode: Esc must not dismiss the ask card")
	}

	// vi mode: Esc while typing an answer must not back out / discard.
	m.chooser = newChooser(ask)
	m.chooser.typing = true
	m.input.SetValue("draft")
	m0, _ := m.Update(esc)
	m = m0.(chatTUI)
	if m.chooser == nil || !m.chooser.typing {
		t.Fatal("vi mode: Esc while typing an ask answer must not back out")
	}
	if got := m.input.Value(); got != "draft" {
		t.Fatalf("vi mode: Esc discarded the draft, value = %q, want draft", got)
	}
}

func TestAskChooserEscStillDismissesWhenNotVi(t *testing.T) {
	ask := event.Ask{
		ID: "ask-1",
		Questions: []event.AskQuestion{{
			ID:      "q1",
			Prompt:  "Pick one",
			Options: []event.AskOption{{Label: "Option A"}},
		}},
	}
	m := newChatTUI(control.New(control.Options{}), "", make(chan event.Event, 1), 80)
	m.cfg = config.Default() // commandmode empty → not vi
	if m.viActive() {
		t.Fatal("setup: expected non-vi mode")
	}
	m.chooser = newChooser(ask)
	out, _ := m.handleChooserKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if out.(chatTUI).chooser != nil {
		t.Fatal("non-vi: Esc must still dismiss the ask card")
	}
}

// TestViEscDoesNotCancelRunningTurn keeps vi mode's Esc a pure mode switch:
// while a turn runs, Esc is a no-op and never cancels — only ^C interrupts.
func TestViEscDoesNotCancelRunningTurn(t *testing.T) {
	r := &blockingTurnRunner{started: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: r, Sink: event.Discard, SessionDir: t.TempDir(), Label: "test"})
	ctrl.Send("hi")
	<-r.started // the turn is in flight and cancellable

	cfg := config.Default()
	cfg.UI.CommandMode = "vi"
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.cfg = cfg
	out, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = out.(chatTUI)
	m.state = tuiRunning

	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = out.(chatTUI)
	if !ctrl.Running() {
		t.Fatal("vi mode: Esc must not cancel a running turn")
	}
	if m.viInCommand() {
		t.Fatal("vi mode: Esc while a turn runs must not enter command mode")
	}

	ctrl.Cancel()
	deadline := time.Now().Add(2 * time.Second)
	for ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatal("turn did not stop after an explicit cancel")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
