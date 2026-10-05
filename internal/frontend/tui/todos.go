package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/frontend/termrender"
)

const todoRows = 8

type todosMsg struct {
	items []TodoItem
	err   error
	// spentDropped says a list that ran to the end is not worth drawing: the
	// screen is being rebuilt from a record, not watching the turn that ended it.
	spentDropped bool
}

func (m *model) fetchTodos() tea.Cmd { return m.readTodos(false) }

func (m *model) fetchTodosForRebuild() tea.Cmd { return m.readTodos(true) }

func (m *model) readTodos(spentDropped bool) tea.Cmd {
	return func() tea.Msg {
		items, err := m.client.Todos(m.ctx)
		return todosMsg{items: items, err: err, spentDropped: spentDropped}
	}
}

func (m *model) onTodos(msg todosMsg) {
	if msg.err != nil {
		return
	}
	m.todos = msg.items
	if msg.spentDropped && todosSpent(m.todos) {
		m.todos = nil
	}
}

// dropSpentTodos clears a finished list as the next turn starts; one with work
// left stays, because the kernel's plan outlives the turn that wrote it.
func (m *model) dropSpentTodos(kind string) {
	if kind == "turn_started" && todosSpent(m.todos) {
		m.todos = nil
	}
}

func todosSpent(todos []TodoItem) bool {
	for _, t := range todos {
		if t.Status != "completed" {
			return false
		}
	}
	return len(todos) > 0
}

// todoLines draws the kernel's task list, a finished one included, until the
// next turn starts: the final N/N is what the turn leaves behind.
func (m *model) todoLines() []string {
	done := 0
	for _, t := range m.todos {
		if t.Status == "completed" {
			done++
		}
	}
	if len(m.todos) == 0 {
		return nil
	}
	lines := []string{termrender.Accent("To-dos") + " " + termrender.Dim(fmt.Sprintf("%d/%d", done, len(m.todos)))}
	start, end := todoWindow(m.todos)
	if start > 0 {
		lines = append(lines, termrender.Dim(fmt.Sprintf("  +%d above", start)))
	}
	for _, t := range m.todos[start:end] {
		indent := "  " + strings.Repeat("  ", max(t.Level, 0))
		text := oneLine(t.Content, m.width-8-len(indent))
		switch t.Status {
		case "completed":
			lines = append(lines, indent+termrender.Green("✔")+" "+termrender.Dim(text))
		case "in_progress":
			lines = append(lines, indent+termrender.Yellow("▶ "+text))
		default:
			lines = append(lines, indent+termrender.Dim("○ "+text))
		}
	}
	if end < len(m.todos) {
		lines = append(lines, termrender.Dim(fmt.Sprintf("  +%d more", len(m.todos)-end)))
	}
	rule := termrender.ThemeFg(termrender.ActiveTheme().Border, strings.Repeat("─", max(m.width-1, 1)))
	out := []string{rule}
	for _, l := range lines {
		out = append(out, " "+l)
	}
	return out
}

// todoWindow keeps the item in progress in view when the list is too long.
func todoWindow(todos []TodoItem) (int, int) {
	if len(todos) <= todoRows {
		return 0, len(todos)
	}
	active := slices.IndexFunc(todos, func(t TodoItem) bool { return t.Status == "in_progress" })
	if active < 0 {
		return 0, todoRows
	}
	start := min(max(active-todoRows/2, 0), len(todos)-todoRows)
	return start, start + todoRows
}
