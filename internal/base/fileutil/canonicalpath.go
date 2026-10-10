package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrAmbiguousPath = errors.New("filesystem path identity is ambiguous")

// CanonicalWritePath resolves existing ancestors while preserving leaf rename
// semantics. Ambiguous missing Windows components cannot establish an identity.
func CanonicalWritePath(path string) (string, error) {
	if runtime.GOOS == "windows" {
		for component := range strings.SplitSeq(filepath.ToSlash(path), "/") {
			if component != "." && component != ".." && strings.TrimRight(component, ". ") != component {
				return "", ErrAmbiguousPath
			}
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", ErrAmbiguousPath
	}
	parent, tail := abs, []string{}
	for {
		resolved, err := ResolveExistingPath(parent)
		if err == nil {
			result := filepath.Join(append([]string{resolved}, tail...)...)
			if len(tail) == 0 && runtime.GOOS == "windows" && strings.EqualFold(filepath.Base(abs), filepath.Base(result)) {
				result = filepath.Join(filepath.Dir(result), filepath.Base(abs))
			}
			return result, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if info, err := os.Lstat(parent); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", ErrAmbiguousPath
		}
		component := filepath.Base(parent)
		if runtime.GOOS == "windows" && (strings.TrimRight(component, ". ") != component || strings.ContainsAny(component, "~:")) {
			return "", ErrAmbiguousPath
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", ErrAmbiguousPath
		}
		tail = append([]string{component}, tail...)
		parent = next
	}
}
