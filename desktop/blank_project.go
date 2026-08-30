package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PickBlankProjectParent opens a folder chooser defaulting to the active
// project's parent, where sibling projects are normally created.
func (a *App) PickBlankProjectParent() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		cur = filepath.Dir(tab.WorkspaceRoot)
	}
	a.mu.RUnlock()
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose where to create the project",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
}

// CreateBlankProject creates one new directory below parentDir and returns its
// absolute path; opening it remains on the existing workspace navigation path.
func (a *App) CreateBlankProject(parentDir, projectName string) (string, error) {
	return createBlankProject(parentDir, projectName)
}

func createBlankProject(parentDir, projectName string) (string, error) {
	parentDir = strings.TrimSpace(parentDir)
	if parentDir == "" {
		return "", fmt.Errorf("parent folder is required")
	}
	if abs, err := filepath.Abs(parentDir); err == nil {
		parentDir = abs
	} else {
		return "", fmt.Errorf("resolve parent folder: %w", err)
	}
	info, err := os.Stat(parentDir)
	if err != nil {
		return "", fmt.Errorf("open parent folder: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("parent path is not a directory: %s", parentDir)
	}

	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		return "", fmt.Errorf("project name is required")
	}
	if projectName == "." || projectName == ".." || strings.ContainsAny(projectName, `/\\`) {
		return "", fmt.Errorf("project name must be a single folder name")
	}
	for _, r := range projectName {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("project name cannot contain control characters")
		}
	}

	target := filepath.Join(parentDir, projectName)
	if filepath.Dir(target) != filepath.Clean(parentDir) {
		return "", fmt.Errorf("project name must be a single folder name")
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("project folder already exists: %s", target)
		}
		return "", fmt.Errorf("create project folder: %w", err)
	}
	return target, nil
}
