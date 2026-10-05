package tui

import (
	"strings"
	"testing"
)

// menuFor puts line in the composer and opens the menu the kernel answered.
func menuFor(m *model, line string, c Completion) {
	m.composer.SetValue(line)
	m.Update(completionMsg{line: line, c: c})
}

func submitted(k *recordingKernel) string {
	for _, call := range k.seen() {
		if strings.HasPrefix(call, "POST /submit ") {
			return call
		}
	}
	return ""
}

// A command typed out in full runs on the first Enter, even when the only rows
// the kernel still offers are fuzzy neighbours: /tree must not become
// /security-review.
func TestEnterRunsACommandTypedInFull(t *testing.T) {
	m, k := testModel(t)
	menuFor(m, "/tree", Completion{Kind: "slash", To: 5,
		Typed: &CompletionItem{Label: "/tree", Insert: "/tree", Hint: "branch tree"},
		Items: []CompletionItem{{Label: "/security-review", Insert: "/security-review "}}})
	if m.menu == nil || m.menu.c.Items[0].Label != "/tree" {
		t.Fatalf("menu = %+v, want /tree first", m.menu)
	}
	run(m, press(m, "enter"))
	if got := submitted(k); !strings.Contains(got, `"input":"/tree"`) {
		t.Fatalf("submitted %q, want /tree; calls:\n%s", got, strings.Join(k.seen(), "\n"))
	}
}

// A command whose menu row descends into arguments still runs as typed: Enter
// does not stop to put its first argument in the composer.
func TestEnterRunsADescendingCommandTypedInFull(t *testing.T) {
	m, k := testModel(t)
	menuFor(m, "/hooks", Completion{Kind: "slash", To: 6,
		Items: []CompletionItem{{Label: "/hooks", Insert: "/hooks ", Descend: true}}})
	run(m, press(m, "enter"))
	if got := submitted(k); !strings.Contains(got, `"input":"/hooks"`) {
		t.Fatalf("submitted %q, want /hooks; composer %q", got, m.composer.Value())
	}
}

// An argument already typed out in full is sent, not accepted a second time.
func TestEnterSendsWhenTheChosenArgumentIsAlreadyThere(t *testing.T) {
	m, k := testModel(t)
	menuFor(m, "/effort high", Completion{Kind: "slash-arg", From: 8, To: 12,
		Items: []CompletionItem{{Label: "high", Insert: "high"}}})
	run(m, press(m, "enter"))
	if got := submitted(k); !strings.Contains(got, `"input":"/effort high"`) {
		t.Fatalf("submitted %q, want /effort high", got)
	}
}

// A half-typed name still completes first, as it does in 1.x.
func TestEnterCompletesAHalfTypedCommand(t *testing.T) {
	m, k := testModel(t)
	menuFor(m, "/tre", Completion{Kind: "slash", To: 4,
		Items: []CompletionItem{{Label: "/tree", Insert: "/tree"}}})
	run(m, press(m, "enter"))
	if got := m.composer.Value(); got != "/tree" {
		t.Fatalf("composer = %q, want the completed name", got)
	}
	if got := submitted(k); got != "" {
		t.Fatalf("a half-typed command was sent: %q", got)
	}
}
