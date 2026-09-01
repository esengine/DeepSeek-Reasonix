package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"reasonix/internal/fileutil"
)

const (
	cleanupStateVersion = 1
	cleanupStateName    = "cleanup-state.json"

	cleanupStagePrepared     = "prepared"
	cleanupStageUnregistered = "unregistered"
)

type cleanupManifestEntry struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Digest string `json:"digest,omitempty"`
}

type cleanupState struct {
	Version        int                    `json:"version"`
	OriginalRoot   string                 `json:"originalRoot"`
	RegisteredRoot string                 `json:"registeredRoot"`
	DetachedRoot   string                 `json:"detachedRoot"`
	WorktreeBranch string                 `json:"worktreeBranch"`
	WorktreeHead   string                 `json:"worktreeHead"`
	Stage          string                 `json:"stage"`
	Manifest       []cleanupManifestEntry `json:"manifest"`
}

func cleanupJournalPath(metadata mergeMetadata) string {
	return filepath.Join(filepath.Dir(metadata.WorktreeRoot), cleanupStateName)
}

func writeCleanupState(metadata mergeMetadata, state cleanupState) error {
	body, err := encodeCleanupState(state)
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFileStrict(cleanupJournalPath(metadata), body, 0o600); err != nil {
		return fmt.Errorf("publish cleanup state: %w", err)
	}
	return nil
}

func createCleanupState(metadata mergeMetadata, state cleanupState) error {
	body, err := encodeCleanupState(state)
	if err != nil {
		return err
	}
	if err := fileutil.AtomicCreateFile(cleanupJournalPath(metadata), body, 0o600); err != nil {
		return fmt.Errorf("publish initial cleanup state: %w", err)
	}
	return nil
}

func encodeCleanupState(state cleanupState) ([]byte, error) {
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode cleanup state: %w", err)
	}
	body = append(body, '\n')
	return body, nil
}

func readCleanupState(metadata mergeMetadata, expectedHead string) (cleanupState, bool, error) {
	path := cleanupJournalPath(metadata)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return cleanupState{}, false, nil
	}
	if err != nil {
		return cleanupState{}, false, fmt.Errorf("inspect cleanup state: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return cleanupState{}, false, errors.New("cleanup state is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return cleanupState{}, false, errors.New("cleanup state permissions are too broad")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return cleanupState{}, false, fmt.Errorf("read cleanup state: %w", err)
	}
	var state cleanupState
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return cleanupState{}, false, fmt.Errorf("decode cleanup state: %w", err)
	}
	if err := validateCleanupState(metadata, expectedHead, state); err != nil {
		return cleanupState{}, false, err
	}
	return state, true, nil
}

func validateCleanupState(metadata mergeMetadata, expectedHead string, state cleanupState) error {
	if state.Version != cleanupStateVersion {
		return fmt.Errorf("unsupported cleanup state version %d", state.Version)
	}
	if filepath.Clean(state.OriginalRoot) != filepath.Clean(metadata.WorktreeRoot) ||
		state.WorktreeBranch != metadata.WorktreeBranch || state.WorktreeHead != expectedHead {
		return errors.New("cleanup state identity does not match the merge receipt")
	}
	if state.Stage != cleanupStagePrepared && state.Stage != cleanupStageUnregistered {
		return fmt.Errorf("unsupported cleanup stage %q", state.Stage)
	}
	cleanupDir := filepath.Join(filepath.Dir(metadata.WorktreeRoot), ".reasonix-cleanup")
	cleanupInfo, err := os.Lstat(cleanupDir)
	if err != nil || !cleanupInfo.IsDir() || cleanupInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("cleanup quarantine is not a real directory")
	}
	for _, path := range []string{state.RegisteredRoot, state.DetachedRoot} {
		rel, err := filepath.Rel(cleanupDir, filepath.Clean(path))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)) {
			return errors.New("cleanup state path escapes the allocation quarantine")
		}
	}
	if filepath.Clean(state.RegisteredRoot) == filepath.Clean(state.DetachedRoot) {
		return errors.New("cleanup state paths are not distinct")
	}
	if state.Manifest == nil {
		return errors.New("cleanup state manifest is missing")
	}
	seen := map[string]struct{}{}
	for _, entry := range state.Manifest {
		if err := validateStatePath(entry.Path); err != nil {
			return fmt.Errorf("invalid cleanup manifest path: %w", err)
		}
		if _, ok := seen[entry.Path]; ok {
			return fmt.Errorf("duplicate cleanup manifest path %q", entry.Path)
		}
		seen[entry.Path] = struct{}{}
	}
	return nil
}

func captureCleanupManifest(ctx context.Context, root string) ([]cleanupManifestEntry, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect cleanup root: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("cleanup root is not a real directory")
	}
	manifest := []cleanupManifestEntry{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if err := validateStatePath(relative); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		item := cleanupManifestEntry{Path: relative, Mode: uint32(info.Mode())}
		switch {
		case info.IsDir():
		case info.Mode().IsRegular():
			digest, err := digestCleanupFile(ctx, path)
			if err != nil {
				return err
			}
			item.Digest = digest
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256([]byte(target))
			item.Digest = hex.EncodeToString(digest[:])
		default:
			return fmt.Errorf("cleanup path %q has unsupported type %s", relative, info.Mode().Type())
		}
		manifest = append(manifest, item)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot cleanup checkout: %w", err)
	}
	sort.Slice(manifest, func(left, right int) bool { return manifest[left].Path < manifest[right].Path })
	return manifest, nil
}

func digestCleanupFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			if _, err := hash.Write(buffer[:count]); err != nil {
				return "", err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func manifestsEqual(expected, actual []cleanupManifestEntry) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return false
		}
	}
	return true
}

func unexpectedCleanupPaths(expected, actual []cleanupManifestEntry) ([]string, error) {
	want := make(map[string]cleanupManifestEntry, len(expected))
	for _, entry := range expected {
		want[entry.Path] = entry
	}
	paths := []string{}
	for _, entry := range actual {
		expectedEntry, ok := want[entry.Path]
		if !ok || expectedEntry != entry {
			paths = append(paths, entry.Path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func safeRemoveDetachedCheckout(ctx context.Context, state cleanupState) ([]MergeBlocker, error) {
	actual, err := captureCleanupManifest(ctx, state.DetachedRoot)
	if err != nil {
		return nil, err
	}
	if paths, _ := unexpectedCleanupPaths(state.Manifest, actual); len(paths) > 0 {
		return []MergeBlocker{{Code: "late_content_preserved", Message: "content changed after checkout detachment and was preserved", Paths: paths}}, errors.New("cleanup_state_changed: detached checkout contains content that must be preserved")
	}
	noteMergeStep("before_cleanup_detached_remove")
	actual, err = captureCleanupManifest(ctx, state.DetachedRoot)
	if err != nil {
		return nil, err
	}
	if paths, _ := unexpectedCleanupPaths(state.Manifest, actual); len(paths) > 0 {
		return []MergeBlocker{{Code: "late_content_preserved", Message: "content changed during checkout cleanup and was preserved", Paths: paths}}, errors.New("cleanup_state_changed: detached checkout contains late content")
	}

	entries := append([]cleanupManifestEntry(nil), state.Manifest...)
	sort.Slice(entries, func(left, right int) bool {
		leftDepth := strings.Count(entries[left].Path, "/")
		rightDepth := strings.Count(entries[right].Path, "/")
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return entries[left].Path > entries[right].Path
	})
	for _, entry := range entries {
		if os.FileMode(entry.Mode).IsDir() {
			continue
		}
		if err := removeExactManifestEntry(ctx, state.DetachedRoot, entry); err != nil {
			return []MergeBlocker{{Code: "late_content_preserved", Message: "changed checkout content was preserved", Paths: []string{entry.Path}}}, fmt.Errorf("cleanup_state_changed: %w", err)
		}
	}
	for _, entry := range entries {
		if !os.FileMode(entry.Mode).IsDir() {
			continue
		}
		if err := removeExactManifestEntry(ctx, state.DetachedRoot, entry); err != nil {
			return []MergeBlocker{{Code: "late_content_preserved", Message: "late checkout content was preserved", Paths: []string{entry.Path}}}, fmt.Errorf("cleanup_state_changed: %w", err)
		}
	}
	if err := os.Remove(state.DetachedRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		remaining, inspectErr := captureCleanupManifest(ctx, state.DetachedRoot)
		paths := []string{"."}
		if inspectErr == nil {
			paths = paths[:0]
			for _, entry := range remaining {
				paths = append(paths, entry.Path)
			}
			if len(paths) == 0 {
				paths = []string{"."}
			}
		}
		return []MergeBlocker{{Code: "late_content_preserved", Message: "the detached checkout was not empty and was preserved", Paths: paths}}, fmt.Errorf("cleanup_state_changed: remove empty detached checkout: %w", err)
	}
	return []MergeBlocker{}, nil
}

func removeExactManifestEntry(ctx context.Context, root string, entry cleanupManifestEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := manifestPath(root, entry.Path)
	if err != nil {
		return err
	}
	actual, err := inspectManifestEntry(ctx, root, entry.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if actual != entry {
		return fmt.Errorf("path %q no longer matches the cleanup manifest", entry.Path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove exact cleanup path %q: %w", entry.Path, err)
	}
	return nil
}

func inspectManifestEntry(ctx context.Context, root, relative string) (cleanupManifestEntry, error) {
	path, err := manifestPath(root, relative)
	if err != nil {
		return cleanupManifestEntry{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return cleanupManifestEntry{}, err
	}
	item := cleanupManifestEntry{Path: relative, Mode: uint32(info.Mode())}
	switch {
	case info.IsDir():
	case info.Mode().IsRegular():
		item.Digest, err = digestCleanupFile(ctx, path)
	case info.Mode()&os.ModeSymlink != 0:
		var target string
		target, err = os.Readlink(path)
		if err == nil {
			digest := sha256.Sum256([]byte(target))
			item.Digest = hex.EncodeToString(digest[:])
		}
	default:
		err = fmt.Errorf("path %q has unsupported type %s", relative, info.Mode().Type())
	}
	return item, err
}

func manifestPath(root, relative string) (string, error) {
	if err := validateStatePath(relative); err != nil {
		return "", err
	}
	parts := strings.Split(filepath.FromSlash(relative), string(filepath.Separator))
	current := root
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("cleanup manifest ancestor %q is not a real directory", current)
		}
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}
