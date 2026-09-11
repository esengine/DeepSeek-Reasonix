package agent

import (
	"path/filepath"
	"strings"

	"reasonix/internal/evidence"
)

// advanceTodoForOperation completes the current task-list item from the real
// tool result instead of waiting for the model to sign it off. Only a todo
// that names the changed file advances: matching on anything looser would make
// the host guess, and a wrong auto-completion is worse than a late one.
func (a *Agent) advanceTodoForOperation(rec evidence.Receipt) {
	if a == nil || !rec.Success || len(rec.Paths) == 0 {
		return
	}
	a.sess.todoMu.Lock()
	index := -1
	for i, todo := range a.sess.todoState {
		if canonicalTodoStatus(todo.Status) != "in_progress" {
			continue
		}
		if todoNamesAnyPath(todo, rec.Paths) {
			index = i
		}
		break
	}
	if index < 0 || !evidence.AdvanceSerialTodo(a.sess.todoState, index) {
		a.sess.todoMu.Unlock()
		return
	}
	snapshot := append([]evidence.TodoItem(nil), a.sess.todoState...)
	a.sess.todoMu.Unlock()
	a.recordTodoState(snapshot)
	a.emitTodoState(snapshot, index+1)
}

// todoNamesAnyPath reports whether the item's own text cites one of the paths
// the host just wrote, by full path or file name.
func todoNamesAnyPath(todo evidence.TodoItem, paths []string) bool {
	text := strings.ToLower(todo.Content + " " + todo.ActiveForm)
	if strings.TrimSpace(text) == "" {
		return false
	}
	for _, path := range paths {
		path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
		if path == "" {
			continue
		}
		if strings.Contains(text, path) {
			return true
		}
		if base := filepath.Base(path); len(base) > 2 && strings.Contains(text, base) {
			return true
		}
	}
	return false
}
