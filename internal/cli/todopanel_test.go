package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRenderTodoPanelNesting proves a level-1 sub-step renders indented under
// its level-0 phase in the pinned task panel.
func TestRenderTodoPanelNesting(t *testing.T) {
	m := newTestChatTUI()
	m.width = 60
	m.todoArgs = `{"todos":[` +
		`{"content":"Phase A","status":"in_progress","level":0},` +
		`{"content":"sub one","status":"pending","level":1}]}`

	out := ansi.Strip(m.renderTodoPanel())
	if !strings.Contains(out, "Phase A") {
		t.Fatalf("panel missing phase:\n%s", out)
	}
	if !strings.Contains(out, "      ○ sub one") {
		t.Fatalf("sub-step not indented under its phase:\n%s", out)
	}
}

func TestRenderTodoPanelScrollsToInProgressTodo(t *testing.T) {
	m := newTestChatTUI()
	m.width = 72
	m.todoArgs = `{"todos":[` +
		`{"content":"Item 01","status":"completed"},` +
		`{"content":"Item 02","status":"completed"},` +
		`{"content":"Item 03","status":"completed"},` +
		`{"content":"Item 04","status":"completed"},` +
		`{"content":"Item 05","status":"completed"},` +
		`{"content":"Item 06","status":"completed"},` +
		`{"content":"Item 07","status":"completed"},` +
		`{"content":"Item 08","status":"completed"},` +
		`{"content":"Item 09","status":"in_progress","activeForm":"Working item 09"},` +
		`{"content":"Item 10","status":"pending"}]}`

	out := ansi.Strip(m.renderTodoPanel())
	if !strings.Contains(out, "Working item 09") {
		t.Fatalf("panel should keep the in-progress todo visible:\n%s", out)
	}
	if strings.Contains(out, "Item 01") {
		t.Fatalf("panel should window around the active todo instead of pinning the first rows:\n%s", out)
	}
}
