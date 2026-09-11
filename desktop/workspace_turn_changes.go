package main

import (
	"reasonix/internal/checkpoint"
	"reasonix/internal/provider"
	"unicode/utf8"
)

type TurnCheckLogView struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

// TurnCheckLog uses the existing archived tool-result reader, scoped to the
// session that supplied the check's exact call ID. It never executes a command.
func (a *App) TurnCheckLog(tabID, sessionPath, toolID, resultID string) *TurnCheckLogView {
	_, ctrl, ok := a.workspaceChangesTarget(tabID)
	if !ok || ctrl == nil || sessionPath == "" || ctrl.SessionPath() != sessionPath || toolID == "" || resultID == "" {
		return nil
	}
	var source *provider.Message
	for _, message := range ctrl.History() {
		if message.Role == provider.RoleTool && message.ID == resultID && message.ToolCallID == toolID {
			source = &message
			break
		}
	}
	if source == nil || ctrl.SessionPath() != sessionPath {
		return nil
	}
	output := source.RawContent
	if output == "" {
		output = source.Content
	}
	truncated := len(output) > workspaceChangeDetailLimit
	if truncated {
		output = output[len(output)-workspaceChangeDetailLimit:]
		for len(output) > 0 && !utf8.RuneStart(output[0]) {
			output = output[1:]
		}
	}
	return &TurnCheckLogView{Output: output, Truncated: truncated}
}

// WorkspaceTurnChanges reads one frozen turn from the addressed session. The
// session path is a correlation token only; it never authorizes reading a path.
func (a *App) WorkspaceTurnChanges(tabID, sessionPath string, turn int, resultID string) *checkpoint.TurnChanges {
	_, ctrl, ok := a.workspaceChangesTarget(tabID)
	if !ok || ctrl == nil || turn < 0 || sessionPath == "" || ctrl.SessionPath() != sessionPath {
		return (*checkpoint.Store)(nil).TurnChanges(turn)
	}
	result := ctrl.CheckpointTurnChanges(turn)
	if ctrl.SessionPath() != sessionPath || resultID == "" || result.ID != resultID {
		return (*checkpoint.Store)(nil).TurnChanges(turn)
	}
	return result.Summary()
}

// WorkspaceTurnChangeDetail only returns a path already present in that turn.
func (a *App) WorkspaceTurnChangeDetail(tabID, sessionPath string, turn int, resultID, path string) *checkpoint.TurnFile {
	_, ctrl, ok := a.workspaceChangesTarget(tabID)
	if !ok || ctrl == nil || turn < 0 || sessionPath == "" || ctrl.SessionPath() != sessionPath {
		return nil
	}
	result := ctrl.CheckpointTurnChanges(turn)
	if ctrl.SessionPath() != sessionPath || resultID == "" || result.ID != resultID {
		return nil
	}
	for _, file := range result.Files {
		if file.Path == path {
			return &file
		}
	}
	return nil
}
