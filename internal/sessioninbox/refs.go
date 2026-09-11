package sessioninbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxFrozenDirEntries = 100

// FreezeRefs snapshots explicit workspace paths without following symlinks.
// Controller-owned enqueue uses its typed resolver; this remains for old/API
// callers that provide an explicit path list.
func FreezeRefs(ctx context.Context, workspace string, paths []string) ([]RefSnapshot, error) {
	_ = ctx
	rootPath, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil || strings.TrimSpace(workspace) == "" {
		return nil, fmt.Errorf("path requires workspace root")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("open workspace root: %w", err)
	}
	defer root.Close()

	out := make([]RefSnapshot, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		snapshot, freezeErr := freezeScopedRef(root, rootPath, path)
		if freezeErr != nil {
			snapshot = RefSnapshot{
				Kind:        "frozen",
				Path:        path,
				DisplayPath: path,
				Content:     fmt.Appendf(nil, "/* ref freeze failed: %v */", freezeErr),
			}
		}
		out = append(out, snapshot)
	}
	return out, nil
}

func freezeScopedRef(root *os.Root, rootPath, path string) (RefSnapshot, error) {
	rel, err := scopedRelativePath(rootPath, path)
	if err != nil {
		return RefSnapshot{}, err
	}
	info, err := root.Lstat(rel)
	if err != nil {
		return RefSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return RefSnapshot{}, fmt.Errorf("path escapes workspace through symlink: %s", path)
	}
	if info.IsDir() {
		content, err := freezeDirectory(root, rel)
		if err != nil {
			return RefSnapshot{}, err
		}
		return frozenSnapshot(rel, content, false), nil
	}

	file, err := root.Open(rel)
	if err != nil {
		return RefSnapshot{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return RefSnapshot{}, err
	}
	if !os.SameFile(info, opened) {
		return RefSnapshot{}, fmt.Errorf("reference changed while opening: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, DefaultMaxItemBytes+1))
	if err != nil {
		return RefSnapshot{}, err
	}
	truncated := len(data) > DefaultMaxItemBytes
	if truncated {
		data = data[:DefaultMaxItemBytes]
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return RefSnapshot{}, fmt.Errorf("reference changed while reading: %s", path)
	}
	return frozenSnapshot(rel, data, truncated), nil
}

func scopedRelativePath(rootPath, path string) (string, error) {
	candidate := filepath.Clean(path)
	if filepath.IsAbs(candidate) {
		rel, err := filepath.Rel(rootPath, candidate)
		if err != nil || !filepath.IsLocal(rel) {
			return "", fmt.Errorf("path outside workspace: %s", path)
		}
		candidate = rel
	}
	if !filepath.IsLocal(candidate) {
		return "", fmt.Errorf("path outside workspace: %s", path)
	}
	return candidate, nil
}

func freezeDirectory(root *os.Root, rel string) ([]byte, error) {
	var entries []string
	if err := walkFrozenDirectory(root, rel, rel, &entries); err != nil {
		return nil, err
	}
	sort.Strings(entries)
	if len(entries) > maxFrozenDirEntries {
		entries = append(entries[:maxFrozenDirEntries], "…[truncated; directory has more entries]…")
	}
	return []byte(strings.Join(entries, "\n")), nil
}

func walkFrozenDirectory(root *os.Root, dir, base string, entries *[]string) error {
	if len(*entries) > maxFrozenDirEntries {
		return nil
	}
	before, err := root.Lstat(dir)
	if err != nil {
		return err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return fmt.Errorf("directory reference changed while opening: %s", dir)
	}
	opened, err := root.Open(dir)
	if err != nil {
		return err
	}
	after, err := opened.Stat()
	if err != nil || !os.SameFile(before, after) {
		opened.Close()
		return fmt.Errorf("directory reference changed while opening: %s", dir)
	}
	children, err := opened.ReadDir(-1)
	opened.Close()
	if err != nil {
		return err
	}
	for _, child := range children {
		path := filepath.Join(dir, child.Name())
		info, err := root.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || skipFrozenDirEntry(child.Name(), info.IsDir()) {
			continue
		}
		display, relErr := filepath.Rel(base, path)
		if relErr != nil || !filepath.IsLocal(display) {
			return fmt.Errorf("directory entry escapes reference root")
		}
		display = filepath.ToSlash(display)
		if info.IsDir() {
			*entries = append(*entries, display+"/")
			if err := walkFrozenDirectory(root, path, base, entries); err != nil {
				return err
			}
		} else {
			*entries = append(*entries, display)
		}
		if len(*entries) > maxFrozenDirEntries {
			return nil
		}
	}
	return nil
}

func skipFrozenDirEntry(name string, isDir bool) bool {
	if name == ".DS_Store" || name == "Thumbs.db" {
		return true
	}
	if !isDir {
		return false
	}
	switch name {
	case ".git", ".idea", ".vscode", "build", "dist", "node_modules", "__pycache__":
		return true
	default:
		return false
	}
}

func frozenSnapshot(path string, content []byte, truncated bool) RefSnapshot {
	sum := sha256.Sum256(content)
	path = filepath.ToSlash(path)
	return RefSnapshot{
		Kind:        "frozen",
		Path:        path,
		DisplayPath: path,
		Content:     content,
		ContentSHA:  hex.EncodeToString(sum[:]),
		Truncated:   truncated,
	}
}

// ApplyFrozenRefs keeps legacy version-1 blobs readable. New entries persist
// the exact typed reference block in PromptEnvelope.FrozenRefBlock.
func ApplyFrozenRefs(submit string, bodies map[string]string) string {
	if len(bodies) == 0 {
		return submit
	}
	paths := make([]string, 0, len(bodies))
	for path := range bodies {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var b strings.Builder
	b.WriteString(submit)
	b.WriteString("\n\n<!-- frozen inbox references (enqueue-time snapshot) -->\n")
	for _, path := range paths {
		fmt.Fprintf(&b, "\n### @%s\n```\n%s\n```\n", path, bodies[path])
	}
	return b.String()
}

// MaterializeRefs validates stored legacy snapshots without reading live paths.
// Clean-git entries from pre-fix blobs are paused until the user refreshes them.
func MaterializeRefs(_ context.Context, _ string, refs []RefSnapshot) (string, map[string]string, error) {
	bodies := make(map[string]string, len(refs))
	for _, ref := range refs {
		if ref.Kind == "clean_git" {
			return fmt.Sprintf("legacy clean-git reference %s requires refresh", ref.Path), bodies, nil
		}
		if ref.ContentSHA != "" {
			sum := sha256.Sum256(ref.Content)
			if hex.EncodeToString(sum[:]) != ref.ContentSHA {
				return fmt.Sprintf("checksum mismatch for frozen ref %s", ref.Path), bodies, nil
			}
		}
		bodies[firstNonEmpty(ref.DisplayPath, ref.Path)] = string(ref.Content)
	}
	return "", bodies, nil
}
