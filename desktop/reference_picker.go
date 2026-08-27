package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PickReferenceFolder opens a native folder picker and returns the selected
// folder as a workspace-relative path. Reference filters are project-scoped,
// so selections outside the active workspace are rejected.
func (a *App) PickReferenceFolder() (string, error) {
	return a.pickReferencePath(true)
}

// PickReferenceFile opens a native file picker and returns the selected file as
// a workspace-relative path. The frontend stores the path as a file rule.
func (a *App) PickReferenceFile() (string, error) {
	return a.pickReferencePath(false)
}

func (a *App) pickReferencePath(folder bool) (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	base, err := workspaceBaseFromRoot(a.activeWorkspaceRoot())
	if err != nil {
		return "", err
	}
	base = filepath.Clean(base)

	options := runtime.OpenDialogOptions{
		DefaultDirectory: dialogDefaultDirectory(base),
	}
	var selected string
	if folder {
		options.Title = "Choose folder to hide from @ references"
		selected, err = runtime.OpenDirectoryDialog(a.ctx, options)
	} else {
		options.Title = "Choose file to hide from @ references"
		selected, err = runtime.OpenFileDialog(a.ctx, options)
	}
	if err != nil || strings.TrimSpace(selected) == "" {
		return "", err
	}

	selected = filepath.Clean(selected)
	info, err := os.Stat(selected)
	if err != nil {
		return "", err
	}
	if folder && !info.IsDir() {
		return "", errors.New("selected path is not a folder")
	}
	if !folder && info.IsDir() {
		return "", errors.New("selected path is a folder; choose a file")
	}
	return referenceRelativeSelection(base, selected, folder)
}

func referenceRelativeSelection(base, selected string, folder bool) (string, error) {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(selected))
	if err != nil {
		return "", fmt.Errorf("resolve selected reference path: %w", err)
	}
	if rel == "." {
		if folder {
			return "", errors.New("workspace root cannot be hidden as a custom folder rule")
		}
		return "", errors.New("workspace root is not a file")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("selected path must be inside the current workspace")
	}
	return filepath.ToSlash(rel), nil
}
