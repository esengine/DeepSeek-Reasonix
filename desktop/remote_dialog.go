package main

import (
	"os"
	"path/filepath"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// PickRemoteIdentityFile opens a native file dialog and returns the selected
// identity-file path. Browser file inputs intentionally hide absolute paths,
// while the SSH client needs a path that the desktop process can open.
func (a *App) PickRemoteIdentityFile() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	defaultDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		defaultDir = dialogDefaultDirectory(filepath.Join(home, ".ssh"))
	}
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Choose SSH identity file",
		DefaultDirectory: defaultDir,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "SSH identity files", Pattern: "*"},
		},
	})
}
