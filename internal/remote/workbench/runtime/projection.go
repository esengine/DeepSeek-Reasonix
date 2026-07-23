package runtime

import (
	"fmt"

	"reasonix/internal/checkpoint"
	"reasonix/internal/evidence"
	"reasonix/internal/remote/protocol"
)

func sessionCheckpointViews(workspace string, ctrl SessionController) []protocol.CheckpointView {
	provider, ok := ctrl.(interface{ Checkpoints() []checkpoint.Meta })
	if !ok {
		return []protocol.CheckpointView{}
	}
	boundary, _ := ctrl.(interface{ CheckpointHasBoundary(int) bool })
	metas := provider.Checkpoints()
	out := make([]protocol.CheckpointView, 0, len(metas))
	for _, meta := range metas {
		files := make([]string, 0, len(meta.Paths))
		for _, path := range meta.Paths {
			if rel := normalizeWorkspacePath(workspace, path); rel != "" {
				files = append(files, rel)
			}
		}
		prompt := meta.Prompt
		created := meta.Time.UnixMilli()
		if created < 0 {
			created = 0
		}
		canConversation := boundary != nil && boundary.CheckpointHasBoundary(meta.Turn)
		out = append(out, protocol.CheckpointView{
			CheckpointID: protocol.CheckpointID(fmt.Sprintf("checkpoint_%d", meta.Turn)),
			DisplayTurn:  meta.Turn, Prompt: &prompt, Files: files, FileCount: len(files),
			CreatedAtMs: created, CanCode: len(files) > 0, CanConversation: canConversation,
		})
	}
	return out
}

func sessionTodoViews(ctrl SessionController) []protocol.TodoItem {
	provider, ok := ctrl.(interface{ Todos() []evidence.TodoItem })
	if !ok {
		return []protocol.TodoItem{}
	}
	todos := provider.Todos()
	out := make([]protocol.TodoItem, 0, len(todos))
	for _, todo := range todos {
		content := todo.Content
		status := protocol.TodoStatus(todo.Status)
		switch status {
		case protocol.TodoPending, protocol.TodoInProgress, protocol.TodoCompleted:
		default:
			continue
		}
		level := todo.Level
		if level < 0 {
			level = 0
		}
		out = append(out, protocol.TodoItem{Content: &content, Status: status, ActiveForm: todo.ActiveForm, Level: level})
	}
	return out
}
