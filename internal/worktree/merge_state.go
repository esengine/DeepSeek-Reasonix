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

// worktreeStateToken fingerprints dirty filesystem state without touching the
// index or object database. Porcelain -z keeps unusual paths unambiguous.
func worktreeStateToken(ctx context.Context, root string) (string, error) {
	status, stderr, err := runGit(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("list changed paths: %w%s", err, stderrSuffix(stderr))
	}
	paths, err := nulStatusPaths(status)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = io.WriteString(hash, "reasonix-worktree-state-v1\x00")
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
		_, copyErr := io.Copy(hash, file)
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
		status, stderr, err := runGit(ctx, path, "status", "--porcelain=v1", "-z", "--untracked-files=all")
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

func verifyStagedWorktree(ctx context.Context, root, confirmedToken string) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := worktreeStateToken(ctx, root)
		if err != nil {
			return "", err
		}
		if token != confirmedToken {
			return "", errors.New("worktree contents no longer match the confirmed snapshot")
		}
		if _, stderr, err := runGit(ctx, root, "diff", "--quiet", "--"); err != nil {
			return "", fmt.Errorf("unstaged changes remain after git add%s", stderrSuffix(stderr))
		}
		untracked, stderr, err := runGit(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return "", fmt.Errorf("inspect untracked files after git add: %w%s", err, stderrSuffix(stderr))
		}
		if untracked != "" {
			return "", errors.New("untracked files remain after git add")
		}
		tree, stderr, err := gitValue(ctx, root, "write-tree")
		if err != nil {
			return "", fmt.Errorf("record staged tree: %w%s", err, stderrSuffix(stderr))
		}
		if attempt == 1 {
			return tree, nil
		}
	}
	return "", errors.New("could not stabilize staged changes")
}

func verifyAutoCommit(ctx context.Context, root, expectedParent, expectedTree string) (string, error) {
	head, stderr, err := gitValue(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read auto-commit HEAD: %w%s", err, stderrSuffix(stderr))
	}
	line, stderr, err := gitValue(ctx, root, "rev-list", "--parents", "-n", "1", head)
	if err != nil {
		return "", fmt.Errorf("read auto-commit parents: %w%s", err, stderrSuffix(stderr))
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != head || fields[1] != expectedParent {
		return "", errors.New("auto-commit does not have the confirmed HEAD as its unique parent")
	}
	tree, stderr, err := gitValue(ctx, root, "rev-parse", "--verify", head+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("read auto-commit tree: %w%s", err, stderrSuffix(stderr))
	}
	if tree != expectedTree {
		return "", errors.New("auto-commit tree differs from the staged tree")
	}
	return head, nil
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
