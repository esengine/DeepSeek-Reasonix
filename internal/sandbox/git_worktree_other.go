//go:build !darwin && !windows

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// gitWorktreeWriteRoots returns the Git metadata directories that a worktree
// needs for writes such as index.lock, HEAD, and ref updates.
func gitWorktreeWriteRoots(writeRoots []string) []string {
	seen := make(map[string]bool, len(writeRoots))
	var out []string
	for _, root := range writeRoots {
		workspace, err := ResolveAbsPath(root)
		if err != nil {
			continue
		}
		gitEntry, ok := findGitEntry(workspace)
		if !ok {
			continue
		}
		info, err := os.Stat(gitEntry)
		if err != nil || info.IsDir() {
			continue
		}
		admin, err := gitDirFromPointer(gitEntry)
		if err != nil {
			continue
		}
		common, err := gitCommonDir(admin)
		if err != nil || !isStandardGitWorktree(admin, common) {
			continue
		}
		addGitWriteRoot(&out, seen, admin)
		addGitWriteRoot(&out, seen, common)
	}
	return out
}

func isStandardGitWorktree(admin, common string) bool {
	if filepath.Base(common) != ".git" {
		return false
	}
	return pathWithin(admin, filepath.Join(common, "worktrees"))
}

func findGitEntry(start string) (string, bool) {
	info, err := os.Stat(start)
	if err != nil || !info.IsDir() {
		return "", false
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		entry := filepath.Join(dir, ".git")
		if _, err := os.Stat(entry); err == nil {
			return entry, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
	}
}

func gitDirFromPointer(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", os.ErrInvalid
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if target == "" {
		return "", os.ErrInvalid
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return ResolveAbsPath(target)
}

func gitCommonDir(admin string) (string, error) {
	b, err := os.ReadFile(filepath.Join(admin, "commondir"))
	if err != nil {
		return "", err
	}
	target := strings.TrimSpace(string(b))
	if target == "" {
		return "", os.ErrInvalid
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(admin, target)
	}
	return ResolveAbsPath(target)
}

func addGitWriteRoot(out *[]string, seen map[string]bool, root string) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return
	}
	root, err = ResolveAbsPath(root)
	if err != nil || seen[root] {
		return
	}
	seen[root] = true
	*out = append(*out, root)
}
