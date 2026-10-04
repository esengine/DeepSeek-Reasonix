package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
)

func ctrlT(m *model) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	return cmd
}

// A session that still owes a key opens the panel by itself, on the
// connection it is running.
func TestSetupOpensItselfWhenAKeyIsOwed(t *testing.T) {
	m, k := testModel(t)
	run(m, m.checkSetup())
	if !calledWith(k, "GET /provider-setup ") {
		t.Fatalf("never asked whether a key is owed:\n%s", strings.Join(k.seen(), "\n"))
	}
	out := bottomText(m)
	if !strings.Contains(out, i18n.M.SetupTitle) || !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("no panel:\n%s", out)
	}
	if m.setup.selected != 1 {
		t.Fatalf("cursor on %d, want the active connection", m.setup.selected)
	}
}

// /setup and its /auth alias open the panel at any time, and the composer
// stays out of the way until it closes.
func TestSetupAndAuthOpenThePanel(t *testing.T) {
	for _, cmd := range []string{"/setup", "/auth"} {
		m, _ := testModel(t)
		typeText(m, cmd)
		run(m, press(m, "enter"))
		if m.setup == nil || !strings.Contains(bottomText(m), i18n.M.SetupTitle) {
			t.Fatalf("%s did not open the panel:\n%s", cmd, bottomText(m))
		}
		run(m, press(m, "esc"))
		if m.setup != nil {
			t.Fatalf("%s: Esc left the panel open", cmd)
		}
	}
}

// The key is masked on screen, tested without being stored, and saved only on
// Enter, for the connection that was picked.
func TestSetupTestsAndSavesTheKeyForThePickedConnection(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "/setup")
	run(m, press(m, "enter"))
	typeText(m, "alp")
	run(m, press(m, "enter"))
	typeText(m, "sk-secret")
	if out := bottomText(m); strings.Contains(out, "sk-secret") || !strings.Contains(out, "•••••••••") {
		t.Fatalf("key not masked:\n%s", out)
	}
	run(m, ctrlT(m))
	if !calledWith(k, `POST /provider-setup/test {"apiKey":"sk-secret","provider":"alpha"}`) {
		t.Fatalf("calls:\n%s", strings.Join(k.seen(), "\n"))
	}
	if calledWith(k, "POST /provider-setup/connection") {
		t.Fatal("a test saved the key")
	}
	run(m, press(m, "enter"))
	if !calledWith(k, `POST /provider-setup/connection {"apiKey":"sk-secret","provider":"alpha","revision":"r1"}`) {
		t.Fatalf("calls:\n%s", strings.Join(k.seen(), "\n"))
	}
	if m.setup != nil {
		t.Fatal("panel still open after saving")
	}
}

func TestSetupRefusesAnEmptyKeyAndEscCancels(t *testing.T) {
	m, k := testModel(t)
	typeText(m, "/setup")
	run(m, press(m, "enter"))
	run(m, press(m, "enter"))
	run(m, press(m, "enter"))
	if calledWith(k, "POST /provider-setup/connection") {
		t.Fatal("saved an empty key")
	}
	run(m, press(m, "esc"))
	if m.setup != nil {
		t.Fatal("Esc did not close the key field")
	}
}

// A key copied with its line ending is the key: it lands in the field, never in
// the composer the panel hides, and nothing is sent to the model when the panel
// closes.
func TestSetupPasteWithLineEndingStaysInThePanel(t *testing.T) {
	for _, ending := range []string{"\n", "\r", "\r\n"} {
		m, k := testModel(t)
		typeText(m, "/setup")
		run(m, press(m, "enter"))
		run(m, press(m, "enter"))
		m.Update(tea.PasteMsg{Content: "sk-pasted" + ending})
		if m.setup.key != "sk-pasted" {
			t.Fatalf("%q: key = %q", ending, m.setup.key)
		}
		m.Update(tea.PasteMsg{Content: "a\nb"})
		run(m, press(m, "esc"))
		if got := m.composer.Value(); got != "" {
			t.Fatalf("%q: composer holds %q after the panel closed", ending, got)
		}
		run(m, press(m, "enter"))
		if calledWith(k, "POST /submit") {
			t.Fatalf("%q: something was submitted:\n%s", ending, strings.Join(k.seen(), "\n"))
		}
	}
}

// Control characters never enter the key, typed or pasted.
func TestSetupKeyRejectsControlCharacters(t *testing.T) {
	m, _ := testModel(t)
	typeText(m, "/setup")
	run(m, press(m, "enter"))
	run(m, press(m, "enter"))
	m.Update(tea.PasteMsg{Content: "sk-\x1b[31mred"})
	m.Update(tea.KeyPressMsg{Code: 0x1b, Text: "\x1b"})
	m.Update(tea.PasteMsg{Content: "sk-ok"})
	if m.setup.key != "sk-ok" {
		t.Fatalf("key = %q", m.setup.key)
	}
}
