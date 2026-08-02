package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
	// Compact: the column hugs its content (header + 4 items, open rails, no
	// top/bottom frame), never stretching to the passed height.
	if lines := strings.Count(out, "\n") + 1; lines != 5 {
		t.Fatalf("sidebar height = %d rows, want 5 (compact content height)", lines)
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
	if !strings.Contains(out, "Tasks 1/1") || !strings.Contains(out, "done") {
		t.Fatalf("finished sidebar should show the completed list:\n%s", out)
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
