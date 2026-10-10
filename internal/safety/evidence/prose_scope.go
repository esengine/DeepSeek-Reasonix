package evidence

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
)

// Prose deliberately interpreted as code or consumed by an external build is
// an accepted limit of this exemption.
// The waiver removes an owed generic check; it never claims verification happened.
func proseMutationPath(root, path string) bool {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(path) == "" || !IsProsePath(path) {
		return false
	}
	if slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "..") {
		return false
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err = filepath.Abs(path)
	if err != nil || !insideProseRoot(root, path) {
		return false
	}
	return unlinkedMutationPath(root, path)
}

func insideProseRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return !fileutil.UnderVCSStore(path)
}

// The scan does not follow links; neither may its exemption. Missing entries
// permit deleted paths while surviving ancestors still have to be unlinked.
func unlinkedMutationPath(root, path string) bool {
	for {
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return false
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return false
		}
		if NormalizePath(path) == NormalizePath(root) {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}
