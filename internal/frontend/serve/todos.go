package serve

import "net/http"

// The canonical task list: the latest todo_write merged with every
// complete_step advance. Not derivable from the transcript — advances write no
// todo_write, and refused todo_writes are written there.

// todoItem is one row as the desktop reads it. Named so wire-parity can hold
// the desktop's hand-written copy to it.
type todoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm,omitempty"`
	Level      int    `json:"level,omitempty"`
}

func (s *Server) todos(w http.ResponseWriter, _ *http.Request) {
	raw := s.ctl().Todos()
	out := make([]todoItem, len(raw))
	for i, t := range raw {
		out[i] = todoItem{Content: t.Content, Status: t.Status, ActiveForm: t.ActiveForm, Level: t.Level}
	}
	writeJSON(w, out)
}
