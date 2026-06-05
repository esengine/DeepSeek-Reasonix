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
