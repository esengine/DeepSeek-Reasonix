package checkpoint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// safePath resolves p against root and rejects anything escaping it — restore
// must never write outside the workspace, even if a snapshot path is hostile or
// the project moved since it was taken.
func safePath(root, p string) (string, error) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, p)
	}
	abs = filepath.Clean(abs)
	if root != "" {
		if err := validateWorkspacePath(root, abs); err != nil {
			return "", err
		}
	}
	return abs, nil
}

var errSymlinkPath = errors.New("workspace path contains symbolic link")

func workspaceRelative(root, abs string) (string, error) {
	if root == "" {
		return filepath.Clean(abs), nil
	}
	r := filepath.Clean(root)
	rel, err := filepath.Rel(r, filepath.Clean(abs))
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("checkpoint path %q escapes workspace %q", abs, root)
	}
	return rel, nil
}

func splitLocalPath(rel string) []string {
	var parts []string
	for rel != "." && rel != "" {
		dir, base := filepath.Split(rel)
		if base != "" {
			parts = append([]string{base}, parts...)
		}
		rel = filepath.Clean(dir)
		if rel == string(filepath.Separator) {
			break
		}
	}
	return parts
}

func validateWorkspacePath(root, abs string) error {
	if root == "" {
		var err error
		abs, err = filepath.Abs(abs)
		if err != nil {
			return err
		}
		root = filepath.VolumeName(abs) + string(filepath.Separator)
	}
	rel, err := workspaceRelative(root, abs)
	if err != nil {
		return err
	}
	workspace, err := os.OpenRoot(filepath.Clean(root))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer workspace.Close()
	cur := "."
	for _, part := range splitLocalPath(rel) {
		cur = filepath.Join(cur, part)
		info, statErr := workspace.Lstat(cur)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", errSymlinkPath, cur)
		}
	}
	return nil
}

func writeNewFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}
