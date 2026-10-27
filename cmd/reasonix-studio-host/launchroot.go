package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/serve"
)

// launchWorkspace is the folder the first pane opens: a chosen directory, then
// a remembered project, then the app's own auto workspace, never the home
// directory. The working directory counts only when someone chose it: a Start-menu or Finder launch inherits the
// install directory or the filesystem root, and taking that as a project puts
// the app's own folder in the sidebar again after every removal.
func launchWorkspace(appExe string) string {
	return pickLaunchWorkspace(boot.ResolveWorkspaceRoot(""), appExe, serve.LaunchWorkspaces(), config.AutoWorkspaceDir())
}

func pickLaunchWorkspace(cwd, appExe string, remembered []string, auto string) string {
	if userChoseDir(cwd, appExe) {
		return cwd
	}
	for _, dir := range remembered {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
	}
	if auto != "" && os.MkdirAll(auto, 0o755) == nil {
		return auto
	}
	return cwd
}

func userChoseDir(cwd, appExe string) bool {
	if cwd == "" || filepath.Dir(cwd) == cwd {
		return false
	}
	rel, err := filepath.Rel(filepath.Dir(appExe), cwd)
	if err != nil {
		return true
	}
	inside := rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
	return !inside
}
