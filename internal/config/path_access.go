package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveConfigAccessPath resolves a config file symlink before any content is
// read or written. User config links are an explicit user choice and may target
// any valid file. Project links are repository-controlled, so their final target
// must stay within the project root (the directory containing reasonix.toml).
func resolveConfigAccessPath(path string, userConfig bool) (string, error) {
	if resolved, ok := pinnedConfigEditPath(path); ok {
		return resolved, nil
	}
	return resolveConfigAccessPathUnpinned(path, userConfig)
}

func resolveConfigAccessPathUnpinned(path string, userConfig bool) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("config path is empty")
	}
	logical, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", path, err)
	}
	resolved, err := evalSymlinksAllowMissing(logical)
	if err != nil {
		scope := "project"
		if userConfig {
			scope = "user"
		}
		return "", fmt.Errorf("resolve %s config path %q: %w", scope, logical, err)
	}
	resolved = filepath.Clean(resolved)
	if userConfig {
		return resolved, nil
	}

	root, err := evalSymlinksAllowMissing(filepath.Dir(logical))
	if err != nil {
		return "", fmt.Errorf("resolve project root %q: %w", root, err)
	}
	root = filepath.Clean(root)
	if !pathWithinRoot(root, resolved) {
		return "", fmt.Errorf("project config path %q resolves outside project root %q: %q", logical, root, resolved)
	}
	return resolved, nil
}

// evalSymlinksAllowMissing canonicalizes every existing path component while
// allowing a new file (and missing parent directories) to be created later.
// A broken final symlink is not "missing": EvalSymlinks sees the link and
// returns an error, which prevents a write from replacing it.
func evalSymlinksAllowMissing(path string) (string, error) {
	path = filepath.Clean(path)
	current := path
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return path, nil
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func resolveConfigReadPath(path string) (string, error) {
	return resolveConfigAccessPath(path, isUserConfigPath(path))
}

func statConfigPath(path string) (resolved string, exists bool, err error) {
	resolved, err = resolveConfigReadPath(path)
	if err != nil {
		return "", false, err
	}
	if _, err = os.Stat(resolved); err != nil {
		if os.IsNotExist(err) {
			return resolved, false, nil
		}
		return "", false, err
	}
	return resolved, true, nil
}

func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
