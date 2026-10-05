package tui

import (
	"strings"
	"testing"
)

// /paste-image is the clipboard of this machine, so the terminal answers it
// itself; sending it to the kernel would call it an unknown command.
func TestPasteImageCommandStaysLocal(t *testing.T) {
	m, k := testModel(t)
	m.composer.SetValue("/paste-image")
	_ = press(m, "enter")
	if m.composer.Value() != "" || strings.Contains(strings.Join(k.seen(), "\n"), "POST /submit") {
		t.Fatalf("composer %q, calls %q", m.composer.Value(), k.seen())
	}
	m.composer.SetValue("/paste-i")
	m.onCompletion(completionMsg{line: "/paste-i"})
	if m.menu == nil || m.menu.c.Items[0].Label != "/paste-image" {
		t.Fatalf("menu = %+v", m.menu)
	}
}
