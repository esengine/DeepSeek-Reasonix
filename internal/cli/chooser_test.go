package cli

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// TestRowLineSelectedChipPadding pins the shared selectable-row look: the
// current row wears the reverse accent chip with a padding space on each side,
// and unselected rows carry the same padding so the columns don't jump as the
// cursor moves.
func TestRowLineSelectedChipPadding(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	refreshCLIStyles()

	sel := rowLine(true, 1, "", "Allow once", false)
	if got, want := ansi.Strip(sel), "❯  1. Allow once "; got != want {
		t.Fatalf("selected row = %q, want %q", got, want)
	}
	chipBG := "48;5;" + strconv.Itoa(activeCLITheme.accent.xterm)
	if !strings.Contains(sel, chipBG) {
		t.Fatalf("selected row should wear the accent chip background (%s): %q", chipBG, sel)
	}

	unsel := rowLine(false, 2, "", "Deny", false)
	if got, want := ansi.Strip(unsel), "   2. Deny "; got != want {
		t.Fatalf("unselected row = %q, want matching padding %q", got, want)
	}
	if strings.Contains(unsel, chipBG) {
		t.Fatalf("unselected row must not wear the chip: %q", unsel)
	}

	// NO_COLOR: the chip degrades to reverse video but the padding stays.
	activeColorProfile = colorprofile.NoTTY
	refreshCLIStyles()
	if got, want := ansi.Strip(rowLine(true, 1, "", "Allow once", false)), "❯  1. Allow once "; got != want {
		t.Fatalf("NO_COLOR selected row = %q, want %q", got, want)
	}
}

// TestChooserEnterOnUnselectedMultiRowKeepsAnswer pins the "choosing cancels the
// turn" bug: Enter on an unselected row of a multi-select question used to
// advance without selecting, submitting an empty answer that AnswerQuestion
// reads as a skip and cancels a running turn. Enter must select the row, so the
// ask resolves with a non-empty selection.
func TestChooserEnterOnUnselectedMultiRowKeepsAnswer(t *testing.T) {
	askCh := make(chan event.Ask, 1)
	ctrl := control.New(control.Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				select {
				case askCh <- e.Ask:
				default:
				}
			}
		}),
	})

	type result struct {
		answers []event.AskAnswer
		err     error
	}
	got := make(chan result, 1)
	go func() {
		answers, err := ctrl.Ask(context.Background(), []event.AskQuestion{{
			ID: "pick", Prompt: "pick one", Multi: true,
			Options: []event.AskOption{{Label: "first"}, {Label: "second"}},
		}})
		got <- result{answers, err}
	}()

	// The ask tool emits an AskRequest before blocking; the chooser keys on its ID.
	var ask event.Ask
	select {
	case ask = <-askCh:
	case <-time.After(2 * time.Second):
		t.Fatal("no AskRequest emitted by ctrl.Ask")
	}

	m := newTestChatTUI()
	m.ctrl = ctrl
	m.chooser = newChooser(ask)
	m.chooser.cursor = 1 // highlight "second", which is not yet selected

	next, _ := m.handleChooserKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if next.(chatTUI).chooser != nil {
		t.Fatal("single-question chooser should close after Enter")
	}

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("Ask: %v", r.err)
		}
		if len(r.answers) != 1 || len(r.answers[0].Selected) != 1 || r.answers[0].Selected[0] != "second" {
			t.Fatalf("answers = %+v, want [second] selected (Enter must pick the highlighted row)", r.answers)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask never resolved — the answer was dropped")
	}
}
