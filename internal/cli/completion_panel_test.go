package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// completionPanelFixture builds a chatTUI with an active slash completion menu
// of n items at a known terminal size.
func completionPanelFixture(t *testing.T, n, height int) chatTUI {
	t.Helper()
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	m = m0.(chatTUI)
	items := make([]compItem, n)
	for i := range items {
		items[i] = compItem{label: "/cmd" + string(rune('a'+i%26)) + string(rune('0'+i/26)), insert: "/cmd "}
	}
	m.completion = completion{active: true, kind: compSlash, items: items, sel: 0, replaceFrom: 0}
	return m
}

func TestCompletionPanelRendersScrollbar(t *testing.T) {
	// A long list renders a completionPanelRows-tall sheet plus the hint
	// footer, with a scrollbar column on the right; a short list gets no
	// scrollbar.
	m := completionPanelFixture(t, 30, 24)
	rows := m.completionPanelRows()
	if rows != 10 { // completionPanelRows (10) < 24 - 10 reserve
		t.Fatalf("completionPanelRows at height 24 = %d, want 10", rows)
	}
	out := m.renderCompletion()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != rows+1 {
		t.Fatalf("render lines = %d, want %d (items + hint)", len(lines), rows+1)
	}
	if !strings.Contains(out, "│") && !strings.Contains(out, "█") {
		t.Fatal("overflowing list should render a scrollbar column")
	}
	// Every item row ends with a scrollbar cell at the content width.
	for _, l := range lines[:rows] {
		if got := visibleWidth(l); got != m.contentWidth() {
			t.Fatalf("item row width = %d, want %d (incl. scrollbar column): %q", got, m.contentWidth(), l)
		}
	}

	m = completionPanelFixture(t, 5, 24)
	out = m.renderCompletion()
	lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 6 { // 5 items + hint
		t.Fatalf("short list render lines = %d, want 6", len(lines))
	}
	if strings.Contains(out, "│") || strings.Contains(out, "█") {
		t.Fatal("short list should not render a scrollbar column")
	}
}

func TestCompletionPanelShrinksOnShortTerminal(t *testing.T) {
	m := completionPanelFixture(t, 30, 12)
	if got := m.completionPanelRows(); got != 4 { // clamped to the 4-row floor
		t.Fatalf("completionPanelRows at height 12 = %d, want 4", got)
	}
	m = completionPanelFixture(t, 3, 24)
	if got := m.completionPanelRows(); got != 3 { // never larger than the list
		t.Fatalf("completionPanelRows for 3 items = %d, want 3", got)
	}
}

func TestCompletionWheelScrollsPicker(t *testing.T) {
	m := completionPanelFixture(t, 30, 24)
	top, _ := m.completionMenuBounds()

	m0, _ := m.Update(tea.MouseWheelMsg{X: 10, Y: top + 1, Button: tea.MouseWheelDown})
	m = m0.(chatTUI)
	if m.completion.sel != 1 {
		t.Fatalf("wheel down over the menu should move selection, sel=%d", m.completion.sel)
	}
	m0, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: top + 1, Button: tea.MouseWheelUp})
	m = m0.(chatTUI)
	if m.completion.sel != 0 {
		t.Fatalf("wheel up over the menu should move selection back, sel=%d", m.completion.sel)
	}

	// A wheel outside the menu bounds keeps scrolling the transcript, not the
	// selection.
	sel := m.completion.sel
	m0, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 1, Button: tea.MouseWheelDown})
	m = m0.(chatTUI)
	if m.completion.sel != sel {
		t.Fatalf("wheel outside the menu must not move selection, sel=%d", m.completion.sel)
	}
}

func TestCompletionScrollbarClickDragRelease(t *testing.T) {
	m := completionPanelFixture(t, 30, 24)
	barX := m.contentWidth() - 1
	top, _ := m.completionMenuBounds()

	// Left-click on the scrollbar column starts a drag and moves the selection.
	m0, _ := m.Update(tea.MouseClickMsg{X: barX, Y: top + 2, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if !m.completionScrollbarDrag {
		t.Fatal("left-click on the completion scrollbar should start a drag")
	}
	if m.completion.sel == 0 {
		t.Fatal("clicking mid-scrollbar should move the selection off the top")
	}

	// Dragging to the panel bottom scrolls the window to the end of the list
	// (the thumb tracks the window, transcript-scrollbar semantics).
	bottom := top + m.completionPanelRows() - 1
	m0, _ = m.Update(tea.MouseMotionMsg{X: barX, Y: bottom, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	windowTop := completionWindowStart(m.completion.sel, len(m.completion.items), m.completionPanelRows())
	if want := len(m.completion.items) - m.completionPanelRows(); windowTop != want {
		t.Fatalf("dragging to the bottom should reach the list end, windowTop=%d want %d (sel=%d)", windowTop, want, m.completion.sel)
	}
	if m.completion.kind != compSlash {
		t.Fatal("scrollbar drag must not disturb the menu kind")
	}

	// Release ends the drag.
	m0, _ = m.Update(tea.MouseReleaseMsg{X: barX, Y: bottom, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.completionScrollbarDrag {
		t.Fatal("mouse release should end the completion scrollbar drag")
	}
}
