package tui

import (
	"strings"
	"testing"

	"reasonix/internal/contract/eventwire"
)

func finishedTodos() []TodoItem {
	return []TodoItem{{Content: "read", Status: "completed"}, {Content: "fix", Status: "completed"}, {Content: "test", Status: "completed"}}
}

// A list that ran to the end stays docked with its final N/N, through an ask
// panel, until the next turn starts.
func TestFinishedTodosStayDockedUntilTheNextTurn(t *testing.T) {
	m, _ := testModel(t)
	m.todos = finishedTodos()
	if v := m.View().Content; !strings.Contains(v, "To-dos") || !strings.Contains(v, "3/3") {
		t.Fatalf("a finished list left the screen when its turn ended:\n%s", v)
	}
	apply(m, askEvent())
	if v := m.View().Content; !strings.Contains(v, "Which database?") || !strings.Contains(v, "3/3") {
		t.Fatalf("the ask panel displaced the list:\n%s", v)
	}
	m.tr.Items = nil
	m.Update(updateMsg{us: []Update{{Event: eventwire.Event{Kind: "turn_started"}}}, ok: true})
	if strings.Contains(m.View().Content, "To-dos") {
		t.Fatal("a finished list outlived the start of the next turn")
	}
}

// A list with work left rides across turns: the kernel owns it, not the turn.
func TestUnfinishedTodosSurviveTheNextTurn(t *testing.T) {
	m, _ := testModel(t)
	m.todos = []TodoItem{{Content: "read", Status: "completed"}, {Content: "fix", Status: "in_progress"}}
	m.Update(updateMsg{us: []Update{{Event: eventwire.Event{Kind: "turn_started"}}}, ok: true})
	if !strings.Contains(m.View().Content, "1/2") {
		t.Fatalf("an unfinished list was dropped:\n%s", m.View().Content)
	}
}

// A screen rebuilt from a record (resume, rewind) does not draw a list that
// finished before it was opened.
func TestRebuiltScreenDropsAFinishedList(t *testing.T) {
	m, _ := testModel(t)
	m.Update(todosMsg{items: finishedTodos(), spentDropped: true})
	if len(m.todos) != 0 {
		t.Fatalf("a finished list came back with the record: %v", m.todos)
	}
	m.Update(todosMsg{items: finishedTodos()})
	if len(m.todos) != 3 {
		t.Fatal("a finished list read after its own turn was dropped")
	}
}
