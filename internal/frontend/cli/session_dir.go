package cli

import (
	"fmt"
	"os"
	"path/filepath"
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

// enterWorkspace switches into dir and answers it as an absolute path, which is
// what a caller hands on as boot's WorkspaceRoot. Empty dir is no override and
// answers empty.
//
// The path is made absolute *before* the switch, and that order is the point: a
// relative value resolved a second time against the directory just changed into
// lands somewhere nobody named. Answering the absolute form is what keeps the
// working directory and the root the same string, so `write_roots` — derived
// from the root — covers the folder the caller asked for.
//
// A directory that cannot be entered is an error rather than a silent fallback,
// because the two answers are different places to work and only the caller
// knows whether to drop the override or refuse the command.
func enterWorkspace(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("--dir %s: %w", dir, err)
	}
	if err := os.Chdir(abs); err != nil {
		return "", fmt.Errorf("--dir %s: %w", dir, err)
	}
	return abs, nil
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
