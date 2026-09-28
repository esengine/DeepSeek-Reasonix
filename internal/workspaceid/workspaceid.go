// Package workspaceid derives the durable key a workspace's personal settings
// are filed under. Linked worktrees of one repository share a key, so a switch
// flipped in the main tree still applies in every worktree cut from it — nine
// worktrees are one project, not nine. Folders outside a repository key by
// path instead. Like environment.WorkspaceVCS this reads the filesystem rather
// than asking git, so it costs nothing on a settings render and cannot flap on
// a slow subprocess.
package workspaceid

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// RepoPrefix and PathPrefix tag which question a key answers, so a stored
// override says how far it reaches without consulting the filesystem again.
const (
	RepoPrefix = "repo:"
	PathPrefix = "path:"
)

// Info describes what a workspace root belongs to. Everything here comes from
// reading files, so a settings render can ask for it without spawning git.
type Info struct {
	Key string // storage key: RepoPrefix or PathPrefix
	// RepoDir is the repository's shared Git directory, identical across linked
	// worktrees, or "" outside a repository. GitDir is this tree's own.
	RepoDir string
	GitDir  string
	Branch  string // checked-out branch, or "" when detached or unreadable
	// Trees counts the working trees sharing RepoDir, the main one included. It
	// is 1 outside a repository.
	Trees int
}

// Describe resolves root's repository identity in one walk.
func Describe(root string) Info {
	base := canonical(root)
	info := Info{Trees: 1}
	if base == "" {
		return info
	}
	for dir := base; ; {
		if common, own, ok := gitDirsAt(filepath.Join(dir, ".git")); ok {
			info.RepoDir, info.GitDir = common, own
			info.Key = RepoPrefix + digest(common)
			info.Branch = branchAt(own)
			info.Trees = countTrees(common)
			return info
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// No repository above root, so the folder itself is the identity —
			// digest base, not the drive the walk ended on.
			info.Key = PathPrefix + digest(base)
			return info
		}
		dir = parent
	}
}

// Key returns the storage key for root: RepoPrefix when root sits inside a Git
// repository, PathPrefix otherwise. An empty root yields an empty key.
func Key(root string) string { return Describe(root).Key }

// RepoDir returns the repository's shared Git directory for root — the same
// path for every linked worktree — or "" when root is not in a repository.
func RepoDir(root string) string { return Describe(root).RepoDir }

// countTrees counts the main tree plus every linked worktree registration.
func countTrees(commonDir string) int {
	entries, err := os.ReadDir(filepath.Join(commonDir, "worktrees"))
	if err != nil {
		return 1
	}
	trees := 1
	for _, entry := range entries {
		if entry.IsDir() {
			trees++
		}
	}
	return trees
}

func branchAt(gitDir string) string {
	body, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(body))
	ref, ok := strings.CutPrefix(head, "ref:")
	if !ok {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
}

// gitDirsAt resolves one .git marker into the shared git dir and this tree's
// own. A directory is the repository itself; a file points at a real git dir,
// which for a linked worktree lives under <common>/worktrees/<name> — cutting
// there is what makes worktrees converge, and a submodule has no such element.
// Both sides canonicalize, or /var/… and /private/var/… read as two repositories.
func gitDirsAt(marker string) (common, own string, ok bool) {
	info, err := os.Lstat(marker)
	if err != nil {
		return "", "", false
	}
	if info.IsDir() {
		dir := canonical(marker)
		return dir, dir, true
	}
	if !info.Mode().IsRegular() {
		return "", "", false
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		return "", "", false
	}
	target := ""
	for line := range strings.SplitSeq(string(body), "\n") {
		if rest, cut := strings.CutPrefix(strings.TrimSpace(line), "gitdir:"); cut {
			target = strings.TrimSpace(rest)
			break
		}
	}
	if target == "" {
		return "", "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(marker), target)
	}
	own = canonical(target)
	return canonical(cutWorktrees(own)), own, true
}

func cutWorktrees(gitDir string) string {
	parts := strings.Split(filepath.ToSlash(gitDir), "/")
	for i := len(parts) - 1; i > 0; i-- {
		if parts[i] == "worktrees" {
			return filepath.FromSlash(strings.Join(parts[:i], "/"))
		}
	}
	return gitDir
}

func canonical(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

// digest keeps the same width worktree.Create already uses for repository keys.
// canonical resolved symlinks first, which on Windows also settles the casing.
func digest(value string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(value)))
	return hex.EncodeToString(sum[:8])
}
