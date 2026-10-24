package checkenv

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/base/fileutil"
)

// denyKey folds case where the host's filesystems usually do, so a forbidden
// root binds every spelling that reaches it; on a case-sensitive volume it
// excludes more than it must, never less.
func denyKey(path string) string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func deniedPath(path string, denied []string) bool {
	key := denyKey(path)
	for _, deny := range denied {
		if key == deny || strings.HasPrefix(key, deny+"/") {
			return true
		}
	}
	return false
}

func relativeDenies(source string, denied []string) ([]string, error) {
	var result []string
	for _, deny := range denied {
		canonical, err := fileutil.CanonicalWritePath(deny)
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(denyKey(canonical), denyKey(source)); err == nil && filepath.IsLocal(rel) {
			return nil, errors.New("verification input is inside a forbidden read root")
		}
		if rel, err := filepath.Rel(denyKey(source), denyKey(canonical)); err == nil && filepath.IsLocal(rel) {
			result = append(result, filepath.ToSlash(rel))
		}
	}
	return result, nil
}
