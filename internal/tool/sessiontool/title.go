package sessiontool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/store"
)

// TitleChangedFunc projects a successful canonical title write into optional
// host-owned compatibility indexes. sessionDir is the boot-scoped root that
// owns sessionPath; callers must keep the projection inside that boundary.
type TitleChangedFunc func(sessionDir, sessionPath, title string) error

type setSessionTitleTool struct {
	sessionDir         string
	currentSessionPath func() string
	onTitleChanged     TitleChangedFunc
}

const setSessionTitleMaxRunes = 120

// NewSetSessionTitleTool creates a host-bound title writer for the current
// conversation. The model never supplies a path, so it cannot rename another
// tab or turn a read-oriented session path resolver into a file-write surface.
func NewSetSessionTitleTool(sessionDir string, currentSessionPath func() string, onTitleChanged TitleChangedFunc) *setSessionTitleTool {
	return &setSessionTitleTool{
		sessionDir:         sessionDir,
		currentSessionPath: currentSessionPath,
		onTitleChanged:     onTitleChanged,
	}
}

func (t *setSessionTitleTool) Name() string   { return "set_session_title" }
func (t *setSessionTitleTool) ReadOnly() bool { return false }
func (t *setSessionTitleTool) Description() string {
	return "Set or clear the current conversation's saved title. Pass an empty title to fall back to the topic title or first-message preview. The host binds the current session; this tool cannot rename other sessions."
}

func (t *setSessionTitleTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "title": {
      "type": "string",
      "maxLength": 120,
      "description": "Short title for the current conversation. Empty clears the explicit session title."
    }
  },
  "required": ["title"]
}`)
}

func (t *setSessionTitleTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if agent.SubagentDepth(ctx) > 0 {
		return "", fmt.Errorf("set_session_title: only the parent conversation can rename itself")
	}
	var params struct {
		Title *string `json:"title"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("set_session_title: invalid args: %w", err)
	}
	if params.Title == nil {
		return "", fmt.Errorf("set_session_title: 'title' argument is required")
	}
	if t == nil || t.currentSessionPath == nil {
		return "", fmt.Errorf("set_session_title: current session is unavailable")
	}
	sessionPath := strings.TrimSpace(t.currentSessionPath())
	if sessionPath == "" {
		return "", fmt.Errorf("set_session_title: current session has no persistent path")
	}
	if !store.IsSessionTranscriptName(filepath.Base(sessionPath)) {
		return "", fmt.Errorf("set_session_title: current session path is not a transcript")
	}
	if agent.IsCleanupPending(sessionPath) {
		return "", fmt.Errorf("set_session_title: current session is pending cleanup")
	}
	title := strings.TrimSpace(*params.Title)
	if len([]rune(title)) > setSessionTitleMaxRunes {
		return "", fmt.Errorf("set_session_title: title exceeds %d characters", setSessionTitleMaxRunes)
	}
	if err := agent.RenameSession(sessionPath, title); err != nil {
		return "", fmt.Errorf("set_session_title: %w", err)
	}
	if t.onTitleChanged != nil {
		if err := t.onTitleChanged(t.sessionDir, sessionPath, title); err != nil {
			return "", fmt.Errorf("set_session_title: update host title index: %w", err)
		}
	}
	if title == "" {
		return "Cleared the current conversation title.", nil
	}
	return fmt.Sprintf("Set the current conversation title to %q.", title), nil
}
