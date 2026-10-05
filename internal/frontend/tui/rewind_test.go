package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
)

func calledWith(k *recordingKernel, prefix string) bool {
	for _, c := range k.seen() {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func bottomText(m *model) string { return strings.Join(m.bottomLines().rows, "\n") }

// A bare /rewind picks a turn first; it never rewinds on its own.
func TestRewindWithoutATurnOpensThePicker(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "/rewind")
	run(m, press(m, "enter"))
	if calledWith(k, "POST /submit") || calledWith(k, "POST /rewind") {
		t.Fatalf("/rewind acted without a choice:\n%s", strings.Join(k.seen(), "\n"))
	}
	if !strings.Contains(bottomText(m), i18n.M.RewindPickTitle) {
		t.Fatalf("no picker:\n%s", bottomText(m))
	}
}

// The picker restores the turn chosen, the way chosen, and hands its prompt
// back to the composer.
func TestRewindPickerRestoresTheChosenTurn(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "/rewind")
	run(m, press(m, "enter"))
	run(m, press(m, "enter"))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	run(m, cmd)
	if !calledWith(k, `POST /rewind/prepare {"scope":"conversation","turn":1}`) || !calledWith(k, `POST /rewind/commit {"planId":"p-1"}`) {
		t.Fatalf("calls:\n%s", strings.Join(k.seen(), "\n"))
	}
	if got := m.composer.Value(); got != "fix the bug" {
		t.Fatalf("composer = %q, want the rewound turn's prompt", got)
	}
}

// /clear deletes the transcript, so it asks, and the answer starts on Cancel.
func TestClearAsksBeforeClearing(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "/clear")
	run(m, press(m, "enter"))
	if !strings.Contains(bottomText(m), i18n.M.SlashClearPrompt) {
		t.Fatalf("no question:\n%s", bottomText(m))
	}
	run(m, press(m, "enter"))
	if calledWith(k, "POST /submit") {
		t.Fatal("Enter on the default answer cleared the context")
	}
	typeText(m, "/clear")
	run(m, press(m, "enter"))
	run(m, press(m, "y"))
	if !calledWith(k, `POST /submit {"input":"/clear"`) {
		t.Fatalf("confirmed /clear never reached the kernel:\n%s", strings.Join(k.seen(), "\n"))
	}
}

// /clear restarts the screen under the banner. The viewport must re-anchor to
// the short new transcript instead of holding the position the old one reached,
// which would leave every transcript row past the content — a blank screen.
func TestClearRestartsTheScreenUnderTheBanner(t *testing.T) {
	m, _ := testModel(t)
	fillTranscript(m, 40)
	m.View()
	typeText(m, "/clear")
	run(m, press(m, "enter"))
	run(m, press(m, "y"))
	v := m.View()
	if strings.Contains(v.Content, "row 39") {
		t.Fatalf("the transcript was not cleared:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "reasonix") {
		t.Fatalf("the screen did not restart under the banner:\n%s", v.Content)
	}
}

// Idle, Esc throws away what is typed; twice on an empty composer it opens the
// rewind picker.
func TestEscClearsTheComposerAndTwiceOpensRewind(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "half a thought")
	run(m, press(m, "esc"))
	if got := m.composer.Value(); got != "" {
		t.Fatalf("composer = %q after Esc", got)
	}
	if calledWith(k, "GET /checkpoints") {
		t.Fatal("clearing the composer also armed the picker")
	}
	run(m, press(m, "esc"))
	run(m, press(m, "esc"))
	if !strings.Contains(bottomText(m), i18n.M.RewindPickTitle) {
		t.Fatalf("double Esc did not open the picker:\n%s", bottomText(m))
	}
}
