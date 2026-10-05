package cli

import (
	"fmt"
	"os"
	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
)

// resolveCLISessionDir is the session dir for a CLI invocation with no --dir.
func resolveCLISessionDir() string { return resolveCLISessionDirFor("") }

// resolveCLISessionDirFor keys the session dir to the workspace root boot will
// resolve from the same input, so a session started in a repository's
// subdirectory is listed with that repository rather than under the
// subdirectory. Falls back to the global session dir.
func resolveCLISessionDirFor(workspaceRoot string) string {
	root := boot.ResolveWorkspaceRoot(workspaceRoot)
	if root == "" {
		return config.SessionDir()
	}
	if projDir := config.ProjectSessionDir(root); projDir != "" && projDir != config.SessionDir() {
		return projDir
	}
	return config.SessionDir()
}

// workspaceRootForDir is the root --dir pins; it runs after chdirTo, so the
// working directory is that root. Empty dir means no override. A Getwd failure
// is returned rather than swallowed: falling back to "" would silently resolve
// the git root instead and break the explicit --dir guarantee.
func workspaceRootForDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve --dir workspace root: %w", err)
	}
	return wd, nil
}
