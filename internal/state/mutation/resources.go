package mutation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
)

func (c *Coordinator) resources(i Intent) ([]string, error) {
	paths := i.Paths
	switch i.Effect {
	case CapacityOnly:
		if len(paths) != 0 || len(i.Expected) != 0 || len(i.Capacities) == 0 || i.Recovery {
			return nil, ErrInvalidIntent
		}
		return nil, nil
	case ReadOnly, External:
		if len(paths) != 0 || len(i.Expected) != 0 || len(i.Capacities) != 0 {
			return nil, ErrInvalidIntent
		}
		return nil, nil
	case Workspace:
		paths = append([]string{c.root}, paths...)
		paths = append(paths, c.workspaceResources...)
	case NamedPaths, GitMetadata:
		if len(paths) == 0 {
			return nil, ErrInvalidIntent
		}
	default:
		return nil, ErrInvalidIntent
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			return nil, ErrInvalidIntent
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(c.root, p)
		}
		canonical, err := fileutil.CanonicalWritePath(p)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidIntent, err)
		}
		out = append(out, canonical)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

func within(root, path string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		root, path = strings.ToLower(root), strings.ToLower(path)
	}
	return withinExact(root, path)
}

func withinExact(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || (rel != "." && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return false
	}
	// filepath.Rel ignores case on Windows, so the spelling has to agree too.
	return strings.HasPrefix(path, root)
}

// heldWithin authorizes path under root. A case-folded spelling counts only
// when the filesystem resolves that prefix to root itself.
func heldWithin(root, path string) bool {
	if withinExact(root, path) {
		return true
	}
	if !within(root, path) {
		return false
	}
	depth := len(strings.Split(filepath.Clean(root), string(filepath.Separator)))
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	if len(parts) < depth {
		return false
	}
	prefix := strings.Join(parts[:depth], string(filepath.Separator))
	if prefix == "" {
		prefix = string(filepath.Separator)
	}
	held, err := os.Stat(root)
	if err != nil {
		return false
	}
	spelled, err := os.Stat(prefix)
	return err == nil && os.SameFile(held, spelled)
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if within(x, y) || within(y, x) {
				return true
			}
		}
	}
	left, right := existing(a), existing(b)
	for _, x := range left {
		for _, y := range right {
			if os.SameFile(x, y) {
				return true
			}
		}
	}
	return false
}

func existing(paths []string) []os.FileInfo {
	out := make([]os.FileInfo, 0, len(paths))
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			out = append(out, info)
		}
	}
	return out
}

func covers(held, requested []string) bool {
	return !slices.ContainsFunc(requested, func(path string) bool {
		return !slices.ContainsFunc(held, func(root string) bool { return heldWithin(root, path) })
	})
}

func (c *Coordinator) validate(ctx context.Context, i Intent, paths []string) error {
	current, err := c.resources(i)
	if err != nil {
		return err
	}
	if !slices.Equal(current, paths) {
		return ErrInvalidIntent
	}
	if c.admit != nil && i.Effect != CapacityOnly {
		i.Paths = current
		if err := c.admit(ctx, i); err != nil {
			return err
		}
	}
	return validateExpected(ctx, i.Expected, paths)
}

// versionProbe rehashes a waiting request's inputs only when their metadata
// moves; the check under a granted lease still reads every version in full.
type versionProbe map[string]os.FileInfo

func (p versionProbe) check(ctx context.Context, versions map[string]ContentVersion, paths []string) error {
	for path, expected := range versions {
		info, statErr := os.Lstat(path)
		if previous, ok := p[path]; ok && statErr == nil && os.SameFile(previous, info) &&
			previous.Size() == info.Size() && previous.Mode() == info.Mode() && previous.ModTime().Equal(info.ModTime()) {
			continue
		}
		if err := validateExpected(ctx, map[string]ContentVersion{path: expected}, paths); err != nil {
			return err
		}
		if statErr == nil {
			p[path] = info
		}
	}
	return nil
}

// A preflight may reject stale inputs, but only the repeated check under a
// granted lease authorizes execution against the expected versions.
func validateExpected(ctx context.Context, versions map[string]ContentVersion, paths []string) error {
	for path, expected := range versions {
		if !filepath.IsAbs(path) {
			return ErrInvalidIntent
		}
		canonical, err := fileutil.CanonicalWritePath(path)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(paths, func(root string) bool { return heldWithin(root, canonical) }) {
			return ErrInvalidIntent
		}
		actual, err := ReadVersionContext(ctx, path)
		if err != nil {
			return err
		}
		if actual != expected {
			return &VersionConflict{Path: path}
		}
	}
	return nil
}
