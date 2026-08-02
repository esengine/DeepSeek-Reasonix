package cli

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

func TestOneLine(t *testing.T) {
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	if got := oneLine("", 10); got != "(empty)" {
		t.Fatalf("empty -> %q", got)
	}
	if got := oneLine("a\nb\nc", 10); strings.Contains(got, "\n") {
		t.Fatalf("oneLine kept a newline: %q", got)
	}
	if got := oneLine("this is a fairly long prompt", 8); len([]rune(got)) > 8 {
		t.Fatalf("oneLine did not truncate to width: %q", got)
	}
}

func TestRenderRewindSmoke(t *testing.T) {
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	metas := []checkpoint.Meta{
		{Turn: 0, Prompt: "add the parser", Paths: []string{"a.go"}},
		{Turn: 1, Prompt: "fix the bug", Paths: []string{"b.go", "c.go"}},
	}
	// Stage 0: turn list.
	m := chatTUI{width: 80, rewind: &rewindPicker{metas: metas, sel: 1}}
	out := m.renderRewind()
	if out == "" || !strings.Contains(out, "Rewind") || !strings.Contains(out, "fix the bug") {
		t.Fatalf("stage-0 render missing content:\n%s", out)
	}
	// Stage 1: scope menu.
	m.rewind.stage = 1
	out = m.renderRewind()
	for _, want := range []string{"Restore to turn 2", "Code + conversation", "Conversation only", "Code only"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stage-1 render missing %q:\n%s", want, out)
		}
	}
	// Closed picker renders nothing.
	m.rewind = nil
	if out := m.renderRewind(); out != "" {
		t.Fatalf("closed picker rendered %q", out)
	}
}

// TestRewindScrollbarClickDragRelease proves the rewind picker's sheet
// scrollbar supports click-drag scrolling over long turn lists.
func TestRewindScrollbarClickDragRelease(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	metas := make([]checkpoint.Meta, 20)
	for i := range metas {
		metas[i] = checkpoint.Meta{Turn: i, Prompt: fmt.Sprintf("turn %d", i)}
	}
	m.rewind = &rewindPicker{metas: metas, sel: 0, stage: 0}
	barX := m.contentWidth() - 1
	rows := m.rewindSheetRows()
	top, _ := m.bottomSheetBounds(rows + 2)

	m0, _ = m.Update(tea.MouseClickMsg{X: barX, Y: top + 2, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.sheetScrollbar == nil || m.sheetScrollbar.panel != "rewind" {
		t.Fatal("left-click on the rewind scrollbar should start a drag")
	}
	if m.rewind.sel == 0 {
		t.Fatal("clicking mid-scrollbar should move the rewind selection")
	}

	bottom := top + 1 + rows - 1
	m0, _ = m.Update(tea.MouseMotionMsg{X: barX, Y: bottom, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	windowTop := completionWindowStart(m.rewind.sel, len(metas), rows)
	if want := len(metas) - rows; windowTop != want {
		t.Fatalf("dragging to the bottom should reach the list end, windowTop=%d want %d (sel=%d)", windowTop, want, m.rewind.sel)
	}

	m0, _ = m.Update(tea.MouseReleaseMsg{X: barX, Y: bottom, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.sheetScrollbar != nil {
		t.Fatal("mouse release should end the rewind scrollbar drag")
	}
}

// TestRewindWheelScrollsSheet proves the wheel over the rewind picker moves
// its selection instead of the transcript.
func TestRewindWheelScrollsSheet(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	metas := make([]checkpoint.Meta, 12)
	for i := range metas {
		metas[i] = checkpoint.Meta{Turn: i, Prompt: fmt.Sprintf("turn %d", i)}
	}
	m.rewind = &rewindPicker{metas: metas, sel: 0, stage: 0}
	rows := m.rewindSheetRows()
	top, _ := m.bottomSheetBounds(rows + 2)

	m0, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: top + 1, Button: tea.MouseWheelDown})
	m = m0.(chatTUI)
	if m.rewind.sel != 1 {
		t.Fatalf("wheel down over the rewind picker should move selection, sel=%d", m.rewind.sel)
	}
}
