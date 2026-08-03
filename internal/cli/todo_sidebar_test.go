package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

const sidebarTodoArgs = `{"todos":[` +
	`{"content":"Add the parser","status":"completed"},` +
	`{"content":"Wire the CLI","status":"in_progress","activeForm":"Wiring the CLI"},` +
	`{"content":"Ship it","status":"pending"},` +
	`{"content":"Polish docs","status":"pending"}]}`

func TestTodoSidebarActive(t *testing.T) {
	m := chatTUI{todoArgs: sidebarTodoArgs}
	// Narrow terminal: the compact bottom panel stays, no sidebar.
	m.width = 99
	if m.todoSidebarActive() {
		t.Fatal("sidebar should be inactive below todoSidebarMinWidth")
	}
	if got := m.contentWidth(); got != 99 {
		t.Fatalf("contentWidth on narrow terminal = %d, want 99", got)
	}
	// Wide terminal with outstanding work: sidebar takes the right column.
	m.width = todoSidebarMinWidth + 40
	if !m.todoSidebarActive() {
		t.Fatal("sidebar should be active on a wide terminal with outstanding work")
	}
	if got := m.contentWidth(); got != todoSidebarMinWidth+40-todoSidebarWidth {
		t.Fatalf("contentWidth = %d, want terminal minus sidebar", got)
	}
	// Wide terminal, list now fully completed: the column persists to show the
	// finished state (all-green bullets) — vanishing here would make the chat
	// column width jump mid-session.
	m.todoArgs = `{"todos":[{"content":"done","status":"completed"}]}`
	if !m.todoSidebarActive() {
		t.Fatal("sidebar should stay active when every item is completed (finished state stays rendered)")
	}
	// No list at all: nothing to show.
	m.todoArgs = ""
	if m.todoSidebarActive() {
		t.Fatal("sidebar should be inactive without a todo list")
	}
}

func TestRenderTodoSidebar(t *testing.T) {
	m := chatTUI{width: 160, todoArgs: sidebarTodoArgs, height: 30}
	out := ansi.Strip(m.renderTodoSidebar(30))
	if out == "" {
		t.Fatal("sidebar rendered empty on a wide terminal with work")
	}
	for _, want := range []string{"Tasks", "1/4", "Add the parser", "Wiring the CLI", "Ship it"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "○ Add the parser") || strings.Contains(out, "▶ Add the parser") {
		t.Fatalf("completed item should render dimmed/checked, got:\n%s", out)
	}
	// The in-progress item uses its activeForm.
	if !strings.Contains(out, "Wiring the CLI") {
		t.Fatalf("sidebar should show the activeForm of the in-progress item:\n%s", out)
	}
	// Compact: the column hugs its content (header + 4 items + inter-item
	// blank rows, open rails, no top/bottom frame), never stretching to the
	// passed height.
	if lines := strings.Count(out, "\n") + 1; lines != 8 {
		t.Fatalf("sidebar height = %d rows, want 8 (header + 4 items + 3 spacing rows)", lines)
	}
}

func TestRenderTodoSidebarInactive(t *testing.T) {
	// Narrow terminal: no sidebar even with a list.
	m := chatTUI{width: 99, todoArgs: sidebarTodoArgs, height: 24}
	if out := m.renderTodoSidebar(24); out != "" {
		t.Fatalf("sidebar on narrow terminal = %q, want empty", out)
	}
	// Wide terminal, no list at all.
	m.width = 160
	m.todoArgs = ""
	if out := m.renderTodoSidebar(24); out != "" {
		t.Fatalf("sidebar without a list = %q, want empty", out)
	}
	// Wide terminal, list fully completed: the finished column still renders.
	m.todoArgs = `{"todos":[{"content":"done","status":"completed"}]}`
	out := ansi.Strip(m.renderTodoSidebar(24))
	if out == "" {
		t.Fatal("sidebar should render the finished list (all-green bullets), not vanish")
	}
	if !strings.Contains(out, "Tasks") || !strings.Contains(out, "▓▓▓▓▓▓▓▓▓▓") || !strings.Contains(out, "1/1") || !strings.Contains(out, "done") {
		t.Fatalf("finished sidebar should show the completed list with a full progress bar:\n%s", out)
	}
}

func TestTodoPanelHiddenOnWideTerminal(t *testing.T) {
	// The pinned bottom panel must not duplicate the sidebar.
	m := chatTUI{width: 160, todoArgs: sidebarTodoArgs}
	if out := m.renderTodoPanel(); out != "" {
		t.Fatalf("bottom todo panel on wide terminal = %q, want empty (sidebar owns it)", out)
	}
	// Narrow terminal keeps the bottom panel.
	m.width = 99
	if out := m.renderTodoPanel(); out == "" {
		t.Fatal("bottom todo panel should render on a narrow terminal")
	}
}

func TestTodoWindowKeepsActiveVisible(t *testing.T) {
	todos := make([]todoPanelTodo, 20)
	for i := range todos {
		todos[i] = todoPanelTodo{Content: "step", Status: "pending"}
	}
	todos[13].Status = "in_progress"
	start, end := todoWindow(todos, 8)
	if end-start != 8 {
		t.Fatalf("window size = %d, want 8", end-start)
	}
	if todos[13].Status != "in_progress" || 13 < start || 13 >= end {
		t.Fatalf("active item %d outside window [%d,%d)", 13, start, end)
	}
	if start != 9 {
		t.Fatalf("window start = %d, want 9 (active centered)", start)
	}
}

func TestRenderTodoSidebarShowsWorkspaceRoot(t *testing.T) {
	// The workspace root is pinned as the sidebar's last row so the panel
	// identifies where the session runs; a missing root (plain fixture) adds
	// no row, keeping the existing compact height contract.
	ctrl := control.New(control.Options{WorkspaceRoot: `C:\repo\DeepSeek-Reasonix`})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 160)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m = m0.(chatTUI)
	m.todoArgs = sidebarTodoArgs
	m0, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m = m0.(chatTUI)
	out := ansi.Strip(m.renderTodoSidebar(30))
	if !strings.Contains(out, "DeepSeek-Reasonix") {
		t.Fatalf("sidebar should pin the workspace root:\n%s", out)
	}
	// The root is separated from the task list by a rail and highlighted with
	// the accent marker, so it can't be mistaken for one more task row.
	if !strings.Contains(out, "◆ C:\\repo\\DeepSeek-Reasonix") {
		t.Fatalf("root row should carry the accent marker:\n%s", out)
	}
	if lines := strings.Count(out, "\n") + 1; lines != 11 {
		t.Fatalf("sidebar with root = %d rows, want 11 (header + 4 items + 4 spacing rows + separator + root)", lines)
	}
}

func TestRenderTodoSidebarWrapsLongTitles(t *testing.T) {
	// Long enough to wrap onto 3 rows at the 44-column content width.
	long := "Implement the complete bidirectional streaming protocol with backpressure handling and reconnect logic"
	m := chatTUI{width: 160, todoArgs: `{"todos":[{"content":"` + long + `","status":"pending"}]}`, height: 30}
	out := ansi.Strip(m.renderTodoSidebar(30))
	// Wrap breaks at word boundaries, so the full title is present word by
	// word (continuation rows keep the inter-word space); an ellipsis would
	// mean the cap truncated it.
	for _, w := range strings.Fields(long) {
		if !strings.Contains(out, w) {
			t.Fatalf("long title word %q missing (truncated?):\n%s", w, out)
		}
	}
	if strings.Contains(out, "…") {
		t.Fatalf("title within the wrap cap must not carry an ellipsis:\n%s", out)
	}
	// 1 header + 3 wrapped rows = 4 (the trailing spacing row is trimmed).
	if lines := strings.Count(out, "\n") + 1; lines != 4 {
		t.Fatalf("sidebar rows = %d, want 4 (header + 3 wrapped title rows)", lines)
	}
}

func TestRenderTodoSidebarCapsWrappedRows(t *testing.T) {
	// A title longer than todoSidebarWrapMax rows is truncated with an
	// ellipsis, and the whole column stays within the terminal height even
	// when every task wraps.
	args := `{"todos":[`
	for i := 0; i < 10; i++ {
		args += `{"content":"step ` + string(rune('a'+i)) + ` ` + strings.Repeat("x", 120) + `","status":"pending"},`
	}
	args = strings.TrimSuffix(args, ",") + `]}`
	m := chatTUI{width: 160, todoArgs: args, height: 24}
	out := ansi.Strip(m.renderTodoSidebar(24))
	if !strings.Contains(out, "+N more") && !strings.Contains(out, "+7 more") {
		// At least the later tasks must be cut off: 10 wrapped titles cannot
		// fit in a 24-row column.
		if lines := strings.Count(out, "\n") + 1; lines <= 20 {
			t.Fatalf("sidebar with 10 long titles should overflow into a +N more footer:\n%s", out)
		}
	}
	if lines := strings.Count(out, "\n") + 1; lines > 24 {
		t.Fatalf("sidebar must not exceed the terminal height: %d rows\n%s", lines, out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("title past the wrap cap should be truncated with an ellipsis:\n%s", out)
	}
}

func TestTodoSidebarWheelScrollsWindow(t *testing.T) {
	todos := make([]todoPanelTodo, 30)
	for i := range todos {
		todos[i] = todoPanelTodo{Content: fmt.Sprintf("step %d", i), Status: "pending"}
	}
	args, err := json.Marshal(struct {
		Todos []todoPanelTodo `json:"todos"`
	}{Todos: todos})
	if err != nil {
		t.Fatal(err)
	}
	m := menuMouseFixture(t, 0, 160, 30, false)
	m.todoArgs = string(args)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m = m0.(chatTUI)
	// A wheel over the sidebar column arms the manual window and scrolls it.
	m0, _ = m.Update(tea.MouseWheelMsg{X: 155, Y: 10, Button: tea.MouseWheelDown})
	m = m0.(chatTUI)
	if m.todoSidebarScroll < 0 {
		t.Fatal("wheel over the sidebar should arm the manual scroll window")
	}
	out := ansi.Strip(m.renderTodoSidebar(30))
	if !strings.Contains(out, "+1 above") {
		t.Fatalf("scrolled window should show the +1 above footer:\n%s", out)
	}
	// Repeated scrolling clamps at the list end: the window never runs past
	// the last task.
	for i := 0; i < 40; i++ {
		m0, _ = m.Update(tea.MouseWheelMsg{X: 155, Y: 10, Button: tea.MouseWheelDown})
		m = m0.(chatTUI)
	}
	_, end := m.scrollWindowBounds(todos, 30)
	if end != len(todos) {
		t.Fatalf("clamped scroll window end = %d, want %d", end, len(todos))
	}
	// A fresh todo_write resets to auto-follow.
	m.todoArgs = sidebarTodoArgs
	m.todoSidebarScroll = -1
	out = ansi.Strip(m.renderTodoSidebar(30))
	if strings.Contains(out, "above") {
		t.Fatalf("auto window with 4 tasks should have no above footer:\n%s", out)
	}
}

// scrollWindowBounds mirrors renderTodoSidebar's manual-window math for
// asserting the clamped end position without rendering.
func (m chatTUI) scrollWindowBounds(todos []todoPanelTodo, height int) (int, int) {
	w := max(height-5, 1)
	start := min(m.todoSidebarScroll, max(len(todos)-w, 0))
	return start, min(start+w, len(todos))
}

func TestWrapTodoLine(t *testing.T) {
	// Short text: single row, no truncation.
	got := wrapTodoLine("short", 20, 3)
	if len(got) != 1 || got[0] != "short" {
		t.Fatalf("short line = %v, want [short]", got)
	}
	// Long text wraps onto multiple rows, all within the width.
	got = wrapTodoLine(strings.Repeat("word ", 8), 10, 3)
	if len(got) != 3 {
		t.Fatalf("wrapped rows = %d, want 3", len(got))
	}
	for _, l := range got {
		if w := visibleWidth(l); w > 10 {
			t.Fatalf("wrapped row %q exceeds width 10 (%d)", l, w)
		}
	}
	// Text beyond the cap is truncated with an ellipsis on the last kept row.
	got = wrapTodoLine(strings.Repeat("x", 100), 10, 2)
	if len(got) != 2 || !strings.HasSuffix(ansi.Strip(got[1]), "…") {
		t.Fatalf("capped rows = %v, want 2 rows ending in an ellipsis", got)
	}
}
