package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// cursorFixture builds a chatTUI at a known size with the todo sidebar active
// (wide terminal + task list), mirroring a live session after the first
// todo_write: the composer has already reflowed to the narrower chat column.
func cursorFixture(t *testing.T, width, height int) chatTUI {
	t.Helper()
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), width)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = m0.(chatTUI)
	m.todoArgs = sidebarTodoArgs
	m0, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = m0.(chatTUI)
	if !m.todoSidebarActive() {
		t.Fatalf("fixture: sidebar inactive at width %d", width)
	}
	return m
}

// TestComposerCursorAlignsWithSidebar pins the terminal-cursor anchor against
// the rendered composer when the todo sidebar narrows the chat column: for a
// single-line, multi-line, and overflowing (internally scrolled) composer, the
// anchored cursor must land on the textarea's cursor row inside the composer
// card — viewport.Height() + rowsAboveBox + 1 border row + the textarea's
// viewport-relative row (no bottom panels are open, so rowsAboveBox is 0).
func TestComposerCursorAlignsWithSidebar(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"single line", "hello"},
		{"multi line", "line one\nline two\nline three"},
		{"overflowing", strings.Repeat("word ", 300)}, // wraps past MaxHeight, textarea scrolls
		{"cjk overflow", strings.Repeat("中文内容测试啊", 60)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := cursorFixture(t, 160, 30)
			m.input.SetValue(tc.value)
			m0, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
			m = m0.(chatTUI)

			ic := m.input.Cursor()
			if ic == nil {
				t.Fatal("focused composer should expose the terminal cursor")
			}
			if ic.Y < 0 || ic.Y >= m.input.Height() {
				t.Fatalf("textarea cursor row %d outside viewport height %d (scrollY=%d)", ic.Y, m.input.Height(), m.input.ScrollYOffset())
			}
			v := m.View()
			if v.Cursor == nil {
				t.Fatal("View should anchor the terminal cursor")
			}
			wantY := m.viewport.Height() + 1 + ic.Y // no bottom panels: rowsAboveBox = 0
			if v.Cursor.Y != wantY {
				t.Fatalf("anchored cursor Y = %d, want %d (viewport %d + border 1 + textarea row %d)", v.Cursor.Y, wantY, m.viewport.Height(), ic.Y)
			}
			// The cursor must sit strictly inside the composer card, above the
			// status block.
			if v.Cursor.Y < m.viewport.Height()+1 || v.Cursor.Y >= m.viewport.Height()+m.input.Height()+2 {
				t.Fatalf("anchored cursor Y = %d outside composer card [%d, %d)", v.Cursor.Y, m.viewport.Height()+1, m.viewport.Height()+m.input.Height()+2)
			}
		})
	}
}

// TestComposerCursorReflowsAfterSidebarToggle pins the width-sync fix: typing a
// long message before the sidebar appears, then opening the sidebar, must
// reflow the textarea (SetWidth) so the cursor row stays inside the visible
// viewport instead of drifting past it — the pre-fix symptom was a cursor
// anchored beyond the composer card.
func TestComposerCursorReflowsAfterSidebarToggle(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 160)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m = m0.(chatTUI)
	// Type a long message while the chat column is still full width.
	m.input.SetValue(strings.Repeat("word ", 200))
	m0, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m = m0.(chatTUI)
	// The sidebar appears: the chat column narrows and the textarea must
	// re-wrap to the new width.
	m.todoArgs = sidebarTodoArgs
	m0, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m = m0.(chatTUI)

	ic := m.input.Cursor()
	if ic == nil {
		t.Fatal("focused composer should expose the terminal cursor")
	}
	if ic.Y < 0 || ic.Y >= m.input.Height() {
		t.Fatalf("after sidebar toggle textarea cursor row %d outside viewport height %d (scrollY=%d)", ic.Y, m.input.Height(), m.input.ScrollYOffset())
	}
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("View should anchor the terminal cursor")
	}
	wantY := m.viewport.Height() + 1 + ic.Y
	if v.Cursor.Y != wantY {
		t.Fatalf("anchored cursor Y = %d, want %d after sidebar toggle", v.Cursor.Y, wantY)
	}
}
