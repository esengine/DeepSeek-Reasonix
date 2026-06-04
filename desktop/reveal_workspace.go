package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func revealWorkspaceCommand(goos string, path string, isDir bool) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", "-R", path)
	case "windows":
		return exec.Command("explorer", "/select,"+path)
	default:
		target := path
		if !isDir {
			target = filepath.Dir(path)
		}
		return exec.Command("xdg-open", target)
	}
}

func revealWorkspacePath(goos string, path string) error {
	info, err := os.Stat(path)
	isDir := err == nil && info.IsDir()
	return revealWorkspaceCommand(goos, path, isDir).Start()
}
