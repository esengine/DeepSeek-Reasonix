// Package files enforces workspace-contained path operations for Remote Workbench.
// Clients pass opaque relative refs; the Host resolves them with no-follow
// semantics and rejects escapes.
package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveRel maps a client-relative ref into an absolute path under workspace.
// Absolute client paths, ".." escapes, and empty workspace are rejected.
// Symlink final targets that leave the workspace are rejected via EvalSymlinks
// when the path exists; for create paths, each existing parent is checked.
func ResolveRel(workspace, rel string) (string, error) {
	ws := filepath.Clean(strings.TrimSpace(workspace))
	if ws == "" || ws == "." {
		return "", fmt.Errorf("workspace root is required")
	}
	absWS, err := filepath.Abs(ws)
	if err != nil {
		return "", err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return absWS, nil
	}
	// The same protocol payload may be produced on Windows and consumed on a
	// Unix Host (or vice versa). filepath.IsAbs only understands the current
	// OS, so reject rooted paths from both path families before cleaning.
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return "", fmt.Errorf("path must be relative to workspace")
	}
	// filepath.VolumeName("C:\\x") is empty on Unix. Keep an explicit drive
	// prefix check so Windows paths are rejected on every Host platform.
	if len(rel) >= 2 && rel[1] == ':' && ((rel[0] >= 'a' && rel[0] <= 'z') || (rel[0] >= 'A' && rel[0] <= 'Z')) {
		return "", fmt.Errorf("path must be relative to workspace")
	}
	cleaned := filepath.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	joined := filepath.Join(absWS, cleaned)
	// Ensure joined stays under absWS even before symlink evaluation.
	relToWS, err := filepath.Rel(absWS, joined)
	if err != nil || relToWS == ".." || strings.HasPrefix(relToWS, ".."+string(filepath.Separator)) || filepath.IsAbs(relToWS) {
		return "", fmt.Errorf("path escapes workspace")
	}
	// If path exists as a symlink leaf, reject. Otherwise return the clean
	// joined path under absWS (do not require EvalSymlinks of the whole tree —
	// macOS temp roots are often symlinked and would false-positive).
	if info, err := os.Lstat(joined); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink paths are not allowed")
		}
	}
	// Creating a new file: ensure each existing parent is not a symlink escape.
	parent := filepath.Dir(joined)
	for parent != absWS && len(parent) > len(absWS) {
		if info, err := os.Lstat(parent); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("symlink parent is not allowed")
			}
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return joined, nil
}

// ListDir returns directory entries under a contained path.
func ListDir(workspace, rel string) ([]os.DirEntry, string, error) {
	path, err := ResolveRel(workspace, rel)
	if err != nil {
		return nil, "", err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, "", err
	}
	return entries, path, nil
}

// ReadFile reads a regular file under workspace with a size limit.
func ReadFile(workspace, rel string, maxBytes int64) ([]byte, error) {
	path, err := ResolveRel(workspace, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds max size")
	}
	return os.ReadFile(path)
}

// WriteFileAtomic writes body via temp file + rename inside the same directory.
func WriteFileAtomic(workspace, rel string, body []byte) error {
	path, err := ResolveRel(workspace, rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".wb-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}
