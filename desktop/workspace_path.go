package main

import (
	"os"
	"path/filepath"
)

// ResolveWorkspacePathForTab returns the absolute local path represented by a
// workspace-relative path or a session-authorized external folder reference.
// Keeping this resolution in the backend ensures the renderer cannot mistake an
// external reference token for a child of the workspace root.
func (a *App) ResolveWorkspacePathForTab(tabID, rel string) (string, error) {
	path, ok, err := a.workspaceOrExternalPathForTab(tabID, rel)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", os.ErrInvalid
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
