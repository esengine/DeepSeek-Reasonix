package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitMetadataLinkedCode identifies a refusal to run confined because a
// protected Git metadata file has another hard link the sandbox cannot cover.
const GitMetadataLinkedCode = "sandbox.git_metadata_linked"

// ErrGitMetadataLinked is returned by CheckGitMetadata for that refusal.
var ErrGitMetadataLinked = errors.New(GitMetadataLinkedCode)

const (
	// gitGroupMaxEntries bounds the worktree or submodule gitdirs given exact
	// rules. Past it Seatbelt falls back to its patterns, so the profile cannot
	// be grown by planting gitdirs.
	gitGroupMaxEntries = 128
	// gitGroupMaxMounts is the larger budget bubblewrap, which has no
	// patterns, spends on exact mounts before binding a whole group read-only.
	gitGroupMaxMounts  = 512
	gitGroupMaxDirs    = 4096
	gitGroupMaxDepth   = 8
	gitHooksMaxEntries = 256
)

// gitGroups is the metadata under a common dir's worktrees/ and modules/:
// exact paths for each gitdir found within the limit, and whether a group
// went past it. A group that is missing or not a real directory is skipped.
type gitGroups struct {
	Paths         []gitProtectedPath
	Worktrees     string
	Modules       string
	WorktreesOver bool
	ModulesOver   bool
}

func gitGroupsOf(common string, limit int) gitGroups {
	var g gitGroups
	if dir := filepath.Join(common, gitWorktreesSubdir); isRealDir(dir) {
		g.Worktrees = dir
		entries, _ := os.ReadDir(dir)
		n := 0
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if n++; n > limit {
				g.WorktreesOver = true
				break
			}
			entry := filepath.Join(dir, e.Name())
			g.Paths = append(g.Paths,
				gitProtectedPath{Path: entry, Pin: true},
				gitProtectedPath{Path: filepath.Join(entry, "config")},
				gitProtectedPath{Path: filepath.Join(entry, "config.worktree")},
				gitProtectedPath{Path: filepath.Join(entry, "commondir")})
		}
	}
	if dir := filepath.Join(common, gitModulesSubdir); isRealDir(dir) {
		g.Modules = dir
		walk := moduleWalk{limit: limit}
		walk.visit(dir, 0)
		g.ModulesOver = walk.overflow
		seen := map[string]bool{}
		for _, gitDir := range walk.gitDirs {
			for d := gitDir; d != dir && !seen[d]; d = filepath.Dir(d) {
				seen[d] = true
				g.Paths = append(g.Paths, gitProtectedPath{Path: d, Pin: true})
			}
			g.Paths = append(g.Paths, gitDirMetadata(gitDir)[1:]...)
		}
	}
	return g
}

// gitGroupPaths is gitGroupsOf with each over-limit group protected as one
// read-only tree, for callers that have no patterns to fall back on.
func gitGroupPaths(common string, limit int) []gitProtectedPath {
	g := gitGroupsOf(common, limit)
	if g.WorktreesOver {
		g.Paths = append(g.Paths, gitProtectedPath{Path: g.Worktrees, Tree: true})
	}
	if g.ModulesOver {
		g.Paths = append(g.Paths, gitProtectedPath{Path: g.Modules, Tree: true})
	}
	return g.Paths
}

func isRealDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// moduleWalk finds submodule git directories, recognised by their HEAD file as
// git recognises a git directory, within a fixed budget. Entries are read
// without following symlinks, so a planted link cannot lead the walk away.
type moduleWalk struct {
	limit    int
	gitDirs  []string
	dirs     int
	overflow bool
}

func (w *moduleWalk) visit(dir string, depth int) {
	if w.overflow || depth > gitGroupMaxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if w.dirs++; w.dirs > gitGroupMaxDirs {
			w.overflow = true
			return
		}
		sub := filepath.Join(dir, e.Name())
		if info, err := os.Lstat(filepath.Join(sub, "HEAD")); err == nil && info.Mode().IsRegular() {
			if len(w.gitDirs) >= w.limit {
				w.overflow = true
				return
			}
			w.gitDirs = append(w.gitDirs, sub)
			w.visit(filepath.Join(sub, gitModulesSubdir), depth+1)
			continue
		}
		w.visit(sub, depth+1)
	}
}

// CheckGitMetadata refuses a confined launch when a protected file has another
// hard link: a write through that name would change the file and no path
// rule can see it. Such a link predates the sandbox, which refuses making one.
func CheckGitMetadata(spec Spec) error {
	linked := gitMetadataLinked(spec)
	if len(linked) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s has another hard link, so a write through that link would change host-protected Git metadata; "+
		"the user has to remove the extra link outside the sandbox (find the workspace -samefile <path>) before commands run confined here",
		ErrGitMetadataLinked, strings.Join(linked, ", "))
}

func gitMetadataLinked(spec Spec) []string {
	meta := gitMetadataForSpec(spec)
	paths := meta.Paths
	for _, common := range meta.Commons {
		paths = append(paths, gitGroupsOf(common, gitGroupMaxEntries).Paths...)
	}
	var linked []string
	for _, p := range paths {
		switch {
		case p.Pin:
		case p.Tree:
			linked = append(linked, linkedFilesUnder(p.Path)...)
		case isLinkedFile(p.Path):
			linked = append(linked, p.Path)
		}
	}
	return linked
}

func linkedFilesUnder(root string) []string {
	var out []string
	seen := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if seen++; seen > gitHooksMaxEntries {
			return filepath.SkipAll
		}
		if !d.IsDir() && isLinkedFile(path) {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func isLinkedFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && linkCount(info) > 1
}
