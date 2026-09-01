package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// worktreeStateToken fingerprints the real index and dirty filesystem state
// without modifying either. Porcelain and ls-files -z keep unusual paths
// unambiguous, while the index entries bind staged-only content and modes.
func worktreeStateToken(ctx context.Context, root string) (string, error) {
	status, stderr, err := runGitEnv(ctx, root, gitNoOptionalLocks, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("list changed paths: %w%s", err, stderrSuffix(stderr))
	}
	index, stderr, err := runGitEnv(ctx, root, gitNoOptionalLocks, "ls-files", "--stage", "-z")
	if err != nil {
		return "", fmt.Errorf("snapshot index entries: %w%s", err, stderrSuffix(stderr))
	}
	paths, err := nulStatusPaths(status)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = io.WriteString(hash, "reasonix-worktree-state-v2\x00status\x00")
	_, _ = io.WriteString(hash, status)
	_, _ = io.WriteString(hash, "\x00index\x00")
	_, _ = io.WriteString(hash, index)
	_, _ = io.WriteString(hash, "\x00filesystem\x00")
	for _, relative := range paths {
		if err := hashWorktreePath(ctx, hash, root, relative); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func nulStatusPaths(status string) ([]string, error) {
	records := strings.Split(status, "\x00")
	seen := map[string]struct{}{}
	paths := []string{}
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			return nil, fmt.Errorf("unexpected Git status record %q", record)
		}
		path := record[3:]
		if err := validateStatePath(path); err != nil {
			return nil, err
		}
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			index++
			if index >= len(records) || records[index] == "" {
				return nil, errors.New("Git status rename record is incomplete")
			}
			oldPath := records[index]
			if err := validateStatePath(oldPath); err != nil {
				return nil, err
			}
			if _, ok := seen[oldPath]; !ok {
				seen[oldPath] = struct{}{}
				paths = append(paths, oldPath)
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func validateStatePath(path string) error {
	if path == "" || filepath.IsAbs(filepath.FromSlash(path)) {
		return fmt.Errorf("unsafe changed path %q", path)
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("changed path escapes worktree: %q", path)
	}
	return nil
}

func hashWorktreePath(ctx context.Context, hash io.Writer, root, relative string) error {
	_, _ = io.WriteString(hash, "path\x00"+relative+"\x00")
	path := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		_, _ = io.WriteString(hash, "deleted\x00")
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect changed path %q: %w", relative, err)
	}
	_, _ = io.WriteString(hash, info.Mode().String()+"\x00")
	switch {
	case info.Mode().IsRegular():
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open changed path %q: %w", relative, err)
		}
		copyErr := copyWithContext(ctx, hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("read changed path %q: %w", relative, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close changed path %q: %w", relative, closeErr)
		}
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("read changed symlink %q: %w", relative, err)
		}
		_, _ = io.WriteString(hash, target)
	case info.IsDir():
		head, stderr, err := gitValue(ctx, path, "rev-parse", "--verify", "HEAD")
		if err != nil {
			return fmt.Errorf("inspect changed Git directory %q: %w%s", relative, err, stderrSuffix(stderr))
		}
		status, stderr, err := runGitEnv(ctx, path, gitNoOptionalLocks, "status", "--porcelain=v1", "-z", "--untracked-files=all")
		if err != nil {
			return fmt.Errorf("inspect changed Git directory status %q: %w%s", relative, err, stderrSuffix(stderr))
		}
		_, _ = io.WriteString(hash, head+"\x00"+status)
	default:
		return fmt.Errorf("changed path %q has unsupported file type %s", relative, info.Mode().Type())
	}
	_, _ = io.WriteString(hash, "\x00")
	return nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) error {
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			if _, err := destination.Write(buffer[:count]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func gitOperation(ctx context.Context, root string) (string, error) {
	operations := []struct{ name, marker string }{
		{"merge", "MERGE_HEAD"}, {"rebase", "rebase-merge"}, {"rebase", "rebase-apply"},
		{"cherry-pick", "CHERRY_PICK_HEAD"}, {"revert", "REVERT_HEAD"}, {"bisect", "BISECT_LOG"},
	}
	for _, operation := range operations {
		path, stderr, err := gitValue(ctx, root, "rev-parse", "--git-path", operation.marker)
		if err != nil {
			return "", fmt.Errorf("inspect Git operation %s: %w%s", operation.name, err, stderrSuffix(stderr))
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if _, err := os.Stat(path); err == nil {
			return operation.name, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect Git operation %s: %w", operation.name, err)
		}
	}
	return "", nil
}
