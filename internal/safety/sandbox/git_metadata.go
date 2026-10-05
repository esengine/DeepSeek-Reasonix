package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// GitMetadataDeniedCode identifies a write the sandbox refused because the
// path is Git configuration or hooks that the host's own git reads.
const GitMetadataDeniedCode = "sandbox.git_metadata_protected"

const (
	gitFileMaxBytes    = 4096
	gitSymlinkMaxHops  = 40
	gitWorktreesSubdir = "worktrees"
	gitModulesSubdir   = "modules"
)

// gitProtectedPath is one host-protected Git metadata path. A tree covers
// everything beneath it; otherwise only the exact entry is protected. A pin
// only holds an entry in place: it cannot be removed, renamed or replaced.
type gitProtectedPath struct {
	Path string
	Tree bool
	Pin  bool
}

// gitMetadata is what a spec protects. Commons are common directories whose
// worktrees/ and modules/ hold further metadata, covered per backend.
type gitMetadata struct {
	Paths   []gitProtectedPath
	Commons []string
}

// GitMetadataPaths lists the Git configuration, pointer and hook paths a
// confined command may not write, as absolute symlink-resolved paths. Hook
// trees end in a separator. Entries that are only pinned in place are omitted.
func GitMetadataPaths(spec Spec) []string {
	meta := gitMetadataForSpec(spec)
	paths := meta.Paths
	for _, common := range meta.Commons {
		paths = append(paths, gitGroupPaths(common, gitGroupMaxEntries)...)
	}
	var out []string
	for _, p := range paths {
		switch {
		case p.Pin:
		case p.Tree:
			out = append(out, p.Path+string(filepath.Separator))
		default:
			out = append(out, p.Path)
		}
	}
	return out
}

func gitMetadataForSpec(spec Spec) gitMetadata {
	if !spec.Enforce() {
		return gitMetadata{}
	}
	writable := writableDirsForSpec(spec)
	if len(writable) == 0 {
		return gitMetadata{}
	}
	return gitMetadataWithin(planWriteRoots(spec, backendWriteDirs(spec)).callers, writable)
}

// gitMetadataWithin resolves the repository git itself would discover from
// each root and keeps what lies inside writable; the rest is unwritable anyway.
func gitMetadataWithin(roots, writable []string) gitMetadata {
	var found []gitProtectedPath
	var commons []string
	for _, root := range roots {
		paths, common := repositoryMetadata(root)
		found = append(found, paths...)
		if common != "" && withinAny(common, writable) && !slices.Contains(commons, common) {
			commons = append(commons, common)
		}
	}
	var out []gitProtectedPath
	seen := map[string]bool{}
	add := func(p gitProtectedPath) {
		if seen[p.Path] || !withinAny(p.Path, writable) {
			return
		}
		seen[p.Path] = true
		out = append(out, p)
	}
	for _, p := range found {
		for _, anc := range writableAncestors(p.Path, writable) {
			add(gitProtectedPath{Path: anc, Pin: true})
		}
		add(p)
	}
	slices.SortFunc(out, func(a, b gitProtectedPath) int { return strings.Compare(a.Path, b.Path) })
	slices.Sort(commons)
	return gitMetadata{Paths: out, Commons: commons}
}

// repositoryMetadata walks up from root the way git's discovery does and
// returns the config, pointer and hook paths of the first `.git` it meets,
// with the common directory git would use for it.
func repositoryMetadata(root string) ([]gitProtectedPath, string) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ""
	}
	dir, err := filepath.EvalSymlinks(root)
	if err != nil || !filepath.IsAbs(dir) {
		return nil, ""
	}
	for {
		entry := filepath.Join(dir, ".git")
		info, err := os.Lstat(entry)
		if err == nil {
			return entryMetadata(dir, entry, info)
		}
		if !os.IsNotExist(err) {
			return nil, ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ""
		}
		dir = parent
	}
}

func entryMetadata(dir, entry string, info os.FileInfo) ([]gitProtectedPath, string) {
	var out []gitProtectedPath
	target := entry
	if info.Mode().IsRegular() {
		out = append(out, gitProtectedPath{Path: entry})
		pointer, ok := readGitPointer(entry, "gitdir: ")
		if !ok {
			return out, ""
		}
		target = pointer
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
	} else {
		out = append(out, gitProtectedPath{Path: entry, Pin: true})
		if stat, err := os.Stat(entry); err != nil || !stat.IsDir() {
			return out, ""
		}
	}
	gitDir, links := resolveWithLinks(target)
	out = append(out, pins(links)...)
	commonDir := gitDir
	if common, ok := readGitPointer(filepath.Join(gitDir, "commondir"), ""); ok {
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		commonDir, links = resolveWithLinks(common)
		out = append(out, pins(links)...)
	}
	out = append(out, gitDirMetadata(gitDir)...)
	if commonDir != gitDir {
		out = append(out, gitDirMetadata(commonDir)...)
	}
	out = append(out,
		gitProtectedPath{Path: filepath.Join(commonDir, gitWorktreesSubdir), Pin: true},
		gitProtectedPath{Path: filepath.Join(commonDir, gitModulesSubdir), Pin: true})
	return out, commonDir
}

func gitDirMetadata(gitDir string) []gitProtectedPath {
	return []gitProtectedPath{
		{Path: gitDir, Pin: true},
		{Path: filepath.Join(gitDir, "config")},
		{Path: filepath.Join(gitDir, "config.worktree")},
		{Path: filepath.Join(gitDir, "commondir")},
		{Path: filepath.Join(gitDir, "hooks"), Tree: true},
	}
}

func pins(paths []string) []gitProtectedPath {
	out := make([]gitProtectedPath, 0, len(paths))
	for _, p := range paths {
		out = append(out, gitProtectedPath{Path: p, Pin: true})
	}
	return out
}

// readGitPointer reads a small regular pointer file. prefix, when set, must
// open the content exactly as git requires of a `.git` file.
func readGitPointer(path, prefix string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > gitFileMaxBytes {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := strings.TrimRight(string(data), " \t\r\n")
	if prefix != "" {
		rest, ok := strings.CutPrefix(text, prefix)
		if !ok {
			return "", false
		}
		text = rest
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

// resolveWithLinks resolves path the way the kernel will, component by
// component, and returns every symlink it passed through: swapping any of
// them would redirect git without touching a protected path. A missing tail
// is kept lexically, so a path that does not exist yet is still named.
func resolveWithLinks(path string) (string, []string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path), nil
	}
	var links []string
	pending := splitPath(abs)
	cur := string(filepath.Separator)
	for hops := 0; len(pending) > 0; {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, part)
		info, err := os.Lstat(next)
		if err != nil {
			return filepath.Join(append([]string{next}, pending...)...), links
		}
		if info.Mode()&os.ModeSymlink == 0 {
			cur = next
			continue
		}
		target, err := os.Readlink(next)
		if hops++; err != nil || hops > gitSymlinkMaxHops {
			return filepath.Join(append([]string{next}, pending...)...), links
		}
		links = append(links, next)
		if filepath.IsAbs(target) {
			cur = string(filepath.Separator)
		}
		pending = append(splitPath(target), pending...)
	}
	return cur, links
}

func splitPath(p string) []string {
	return strings.Split(filepath.ToSlash(p), "/")
}

// writableAncestors are path's ancestors inside a writable directory, outermost
// first. Renaming one away would move the protected path out from under its rule.
func writableAncestors(path string, writable []string) []string {
	var out []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if !withinAny(dir, writable) {
			break
		}
		out = append(out, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	slices.Reverse(out)
	return out
}

// writableDirsForSpec is every directory the backend makes writable for spec,
// symlink-resolved to match the paths rules are written for.
func writableDirsForSpec(spec Spec) []string {
	var out []string
	for _, d := range confinedWriteDirs(spec) {
		if strings.TrimSpace(d) == "" {
			continue
		}
		if real, _ := resolveWithLinks(d); real != "" {
			out = append(out, real)
		}
	}
	return out
}

func withinAny(path string, dirs []string) bool {
	for _, d := range dirs {
		if pathWithin(d, path) {
			return true
		}
	}
	return false
}
