package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// menuMouseFixture builds a chatTUI with an active slash completion menu of n
// items at a known terminal size, optionally with the todo sidebar active
// (wide terminal + a task list).
func menuMouseFixture(t *testing.T, n, width, height int, sidebar bool) chatTUI {
	t.Helper()
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), width)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = m0.(chatTUI)
	if sidebar {
		m.todoArgs = sidebarTodoArgs
		// Re-run the update cycle so the viewport and composer reflow to the
		// now-narrower chat column, exactly as a live session does when the
		// first todo_write arrives.
		m0, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m = m0.(chatTUI)
		if !m.todoSidebarActive() {
			t.Fatalf("fixture: sidebar inactive at width %d", width)
		}
		if got := m.input.Width(); got != m.contentWidth()-6-composerPromptWidth {
			t.Fatalf("fixture: composer width %d not reflowed to content width %d", got, m.contentWidth())
		}
	}
	items := make([]compItem, n)
	for i := range items {
		items[i] = compItem{label: "/cmd" + string(rune('a'+i%26)) + string(rune('0'+i/26)), insert: "/cmd "}
	}
	m.completion = completion{active: true, kind: compSlash, items: items, sel: 0, replaceFrom: 0}
	return m
}

// TestCompletionMenuClickSelectsAndAccepts pins the missing mouse contract for
// the slash menu: a left-click on an item row moves the selection there, and a
// second click on the already-selected row accepts it (applies the insert),
// mirroring Enter. Clicks on the hint footer must not move the selection.
func TestCompletionMenuClickSelectsAndAccepts(t *testing.T) {
	m := menuMouseFixture(t, 5, 80, 24, false)
	top, bottom := m.completionMenuBounds()
	itemRows := m.completionPanelRows()

	// Click the third item row: selection follows the pointer.
	m0, _ := m.Update(tea.MouseClickMsg{X: 10, Y: top + 2, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.completion.sel != 2 {
		t.Fatalf("click on item row 3 should select index 2, sel=%d", m.completion.sel)
	}

	// Click the same row again: accept the selected item (input gets its insert).
	before := m.input.Value()
	m0, _ = m.Update(tea.MouseClickMsg{X: 10, Y: top + 2, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.input.Value() == before {
		t.Fatalf("second click on the selected row should accept the item, input=%q", m.input.Value())
	}

	// Clicking the hint footer row must not move the selection.
	m = menuMouseFixture(t, 5, 80, 24, false)
	sel := m.completion.sel
	m0, _ = m.Update(tea.MouseClickMsg{X: 10, Y: bottom - 1, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.completion.sel != sel {
		t.Fatalf("click on the hint footer must not move the selection, sel=%d", m.completion.sel)
	}

	// A click past the visible item rows (the menu is shorter than the panel)
	// must not move the selection either.
	m = menuMouseFixture(t, 5, 80, 24, false)
	sel = m.completion.sel
	m0, _ = m.Update(tea.MouseClickMsg{X: 10, Y: top + itemRows + 1, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.completion.sel != sel {
		t.Fatalf("click below the visible items must not move the selection, sel=%d", m.completion.sel)
	}
}

// TestCompletionMenuClickWithSidebar verifies the menu's mouse geometry stays
// correct when the todo sidebar shrinks the chat column: item clicks work
// inside the content width, and clicks on the sidebar column itself are never
// interpreted as menu interaction.
func TestCompletionMenuClickWithSidebar(t *testing.T) {
	m := menuMouseFixture(t, 5, 140, 30, true)
	top, _ := m.completionMenuBounds()

	m0, _ := m.Update(tea.MouseClickMsg{X: 10, Y: top + 2, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.completion.sel != 2 {
		t.Fatalf("click on item row 3 with sidebar open should select index 2, sel=%d", m.completion.sel)
	}

	// A click in the sidebar column (X beyond the chat content width) must not
	// be treated as a menu row click.
	sel := m.completion.sel
	sideX := m.width - 5
	if sideX < m.contentWidth() {
		t.Fatalf("fixture: sidebar click X=%d must be outside content width %d", sideX, m.contentWidth())
	}
	m0, _ = m.Update(tea.MouseClickMsg{X: sideX, Y: top + 2, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.completion.sel != sel {
		t.Fatalf("click in the sidebar column must not move menu selection, sel=%d", m.completion.sel)
	}
}

// TestSidebarClickDoesNotTouchComposer pins the hit-test boundary of the
// composer: a click on the todo sidebar column (beyond the chat content width)
// must never be interpreted as a composer click, even when its row overlaps the
// composer vertically — otherwise the composer selection grabs the drag and
// transcript drag-select dies exactly when the sidebar is open.
func TestSidebarClickDoesNotTouchComposer(t *testing.T) {
	m := menuMouseFixture(t, 0, 140, 30, true)
	m.completion = completion{} // no menu; plain layout
	_, originY, ok := m.composerOrigin()
	if !ok {
		t.Fatal("fixture: composer origin unavailable")
	}
	sideX := m.width - 5
	if sideX < m.contentWidth() {
		t.Fatalf("fixture: sidebar click X=%d must be outside content width %d", sideX, m.contentWidth())
	}
	m0, _ := m.Update(tea.MouseClickMsg{X: sideX, Y: originY, Button: tea.MouseLeft})
	m = m0.(chatTUI)
	if m.composerSel.active {
		t.Fatalf("sidebar click must not start a composer selection (composerSel.active=%v)", m.composerSel.active)
	}
}
