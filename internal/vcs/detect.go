package vcs

import (
	"os"
	"path/filepath"
	"strings"
)

// DetectVCS returns the VCS type for the given directory: "git", "jj", or "".
// When both .jj and .git are present (colocated repo), jj is preferred.
func DetectVCS(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	if isDir(filepath.Join(cwd, ".jj")) {
		return "jj"
	}
	if isDir(filepath.Join(cwd, ".git")) {
		return "git"
	}
	// Walk up ancestors looking for .jj or .git.
	dir := cwd
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if isDir(filepath.Join(parent, ".jj")) {
			return "jj"
		}
		if isDir(filepath.Join(parent, ".git")) {
			return "git"
		}
		dir = parent
	}
	return ""
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
