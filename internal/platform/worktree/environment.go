package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/secrets"
)

var ErrEnvironment = errors.New("isolated environment preparation failed")

// EnvironmentReceipt describes copies actually prepared from declared inputs.
// It is not a sandbox proof or a claim that undeclared inputs are covered.
type EnvironmentReceipt struct {
	Version           int      `json:"version"`
	DeclarationDigest string   `json:"declaration_digest"`
	ContentDigest     string   `json:"content_digest"`
	Paths             []string `json:"paths"`
	Entries           int      `json:"entries"`
	Bytes             int64    `json:"bytes"`
}

type environmentEntry struct {
	Path   string      `json:"path"`
	Mode   fs.FileMode `json:"mode"`
	Size   int64       `json:"size"`
	Digest string      `json:"digest,omitempty"`
}

const environmentMaxEntries = 50_000
const environmentMaxBytes = 2 << 30

// PrepareEnvironment copies only explicitly declared repository-local paths.
// Source and destination must be coordinated by the caller. Symlinks, special
// files, credential paths and Git metadata are refused before any copying.
func PrepareEnvironment(ctx context.Context, snap Snapshot, candidate Candidate, paths []string, forbidReadRoots ...string) (*EnvironmentReceipt, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	roots, err := environmentPaths(paths)
	if err != nil {
		return nil, err
	}
	if err := checkEnvironmentReadPolicy(snap.RepoRoot, roots, forbidReadRoots); err != nil {
		return nil, err
	}
	source, err := os.OpenRoot(snap.RepoRoot)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	target, err := os.OpenRoot(candidate.WorktreeRoot)
	if err != nil {
		return nil, err
	}
	defer target.Close()
	entries, total, err := scanEnvironment(ctx, source, roots)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := copyEnvironmentEntry(ctx, source, target, entry); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrEnvironment, entry.Path, err)
		}
	}
	for _, entry := range slices.Backward(entries) {
		if entry.Mode.IsDir() {
			if err := target.Chmod(entry.Path, entry.Mode.Perm()); err != nil {
				return nil, err
			}
		}
	}
	after, afterBytes, err := scanEnvironment(ctx, source, roots)
	if err != nil || afterBytes != total || !slices.Equal(entries, after) {
		return nil, errors.Join(ErrEnvironment, err, ErrWorkspaceMoved)
	}
	prepared, preparedBytes, err := scanEnvironment(ctx, target, roots)
	if err != nil || preparedBytes != total || !slices.Equal(entries, prepared) {
		return nil, errors.Join(ErrEnvironment, err, ErrWorkspaceMoved)
	}
	return &EnvironmentReceipt{Version: 1, DeclarationDigest: environmentDigest(roots),
		ContentDigest: environmentDigest(entries), Paths: roots, Entries: len(entries), Bytes: total}, nil
}

func checkEnvironmentReadPolicy(source string, paths, denied []string) error {
	for _, path := range paths {
		input, err := fileutil.CanonicalWritePath(filepath.Join(source, path))
		if err != nil {
			return errors.Join(ErrEnvironment, err)
		}
		resolvedSource, err := filepath.EvalSymlinks(source)
		if err != nil {
			return errors.Join(ErrEnvironment, err)
		}
		relative, err := filepath.Rel(resolvedSource, input)
		if err != nil || !filepath.IsLocal(relative) {
			return errors.Join(ErrEnvironment, err)
		}
		if err := safeEnvironmentPath(relative); err != nil {
			return err
		}
		for _, deny := range denied {
			resolved, err := filepath.EvalSymlinks(deny)
			if errors.Is(err, os.ErrNotExist) {
				resolved, err = fileutil.CanonicalWritePath(deny)
			}
			if err != nil {
				return errors.Join(ErrEnvironment, err)
			}
			a, errA := filepath.Rel(input, resolved)
			b, errB := filepath.Rel(resolved, input)
			if (errA == nil && filepath.IsLocal(a)) || (errB == nil && filepath.IsLocal(b)) {
				return fmt.Errorf("%w: declared input intersects a forbidden read root", ErrEnvironment)
			}
		}
	}
	return nil
}

func EnvironmentSourceMatches(ctx context.Context, snap Snapshot) error {
	repo, err := snap.repo(ctx)
	if err != nil {
		return err
	}
	tree, err := workingTree(ctx, repo)
	if err != nil {
		return err
	}
	if tree != snap.Tree {
		return ErrWorkspaceMoved
	}
	return nil
}

func environmentPaths(paths []string) ([]string, error) {
	roots := make([]string, 0, len(paths))
	for _, path := range paths {
		if !filepath.IsLocal(path) || filepath.Clean(path) == "." || strings.Contains(path, "\\") {
			return nil, fmt.Errorf("%w: declarations must name repository-local paths", ErrEnvironment)
		}
		path = filepath.Clean(path)
		if err := safeEnvironmentPath(path); err != nil {
			return nil, err
		}
		roots = append(roots, path)
	}
	slices.Sort(roots)
	return slices.Compact(roots), nil
}

func safeEnvironmentPath(path string) error {
	for component := range strings.SplitSeq(filepath.ToSlash(path), "/") {
		if strings.EqualFold(component, ".git") || secrets.SensitiveFileName(component) || sensitiveEnvironmentDir(component) {
			return fmt.Errorf("%w: credential or Git metadata path is unsupported", ErrEnvironment)
		}
	}
	return nil
}

func sensitiveEnvironmentDir(component string) bool {
	switch strings.ToLower(component) {
	case ".ssh", ".gnupg", ".aws", ".azure", ".kube":
		return true
	}
	return false
}

func scanEnvironment(ctx context.Context, source *os.Root, roots []string) ([]environmentEntry, int64, error) {
	byPath := map[string]environmentEntry{}
	var total int64
	add := func(path string) error {
		if _, exists := byPath[path]; exists {
			return nil
		}
		if len(byPath) >= environmentMaxEntries {
			return fmt.Errorf("%w: declared inputs exceed %d entries", ErrEnvironment, environmentMaxEntries)
		}
		entry, err := readEnvironmentEntry(ctx, source, path)
		if err != nil {
			return err
		}
		total += entry.Size
		if total > environmentMaxBytes {
			return fmt.Errorf("%w: declared inputs exceed byte limit", ErrEnvironment)
		}
		byPath[path] = entry
		return nil
	}
	for _, root := range roots {
		for parent := filepath.Dir(root); parent != "."; parent = filepath.Dir(parent) {
			if err := add(parent); err != nil {
				return nil, 0, err
			}
		}
		err := fs.WalkDir(source.FS(), filepath.ToSlash(root), func(path string, _ fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("%w: declared input is unreadable: %w", ErrEnvironment, walkErr)
			}
			return add(filepath.FromSlash(path))
		})
		if err != nil {
			return nil, 0, err
		}
	}
	entries := make([]environmentEntry, 0, len(byPath))
	for _, entry := range byPath {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b environmentEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries, total, nil
}

func environmentDigest(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
