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
	cleanupStateVersion       = 2
	legacyCleanupStateVersion = 1
	cleanupStateName          = "cleanup-state.json"

	cleanupStagePlanned  = "planned"
	cleanupStageRetained = "retained"

	legacyCleanupStagePrepared     = "prepared"
	legacyCleanupStageUnregistered = "unregistered"
)

type cleanupManifestEntry struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Digest string `json:"digest,omitempty"`
}

// cleanupState v2 records a retained, registered recovery checkout. It never
// describes deletion work: old and unknown writers can only preserve it.
type cleanupState struct {
	Version        int    `json:"version"`
	OriginalRoot   string `json:"originalRoot"`
	RecoveryRoot   string `json:"recoveryRoot"`
	WorktreeBranch string `json:"worktreeBranch"`
	WorktreeHead   string `json:"worktreeHead"`
	Stage          string `json:"stage"`
}

// legacyCleanupState is read-only compatibility for v1 journals that may
// have stopped between checkout detachment and physical deletion.
type legacyCleanupState struct {
	Version        int                    `json:"version"`
	OriginalRoot   string                 `json:"originalRoot"`
	RegisteredRoot string                 `json:"registeredRoot"`
	DetachedRoot   string                 `json:"detachedRoot"`
	WorktreeBranch string                 `json:"worktreeBranch"`
	WorktreeHead   string                 `json:"worktreeHead"`
	Stage          string                 `json:"stage"`
	Manifest       []cleanupManifestEntry `json:"manifest"`
}

type cleanupJournal struct {
	Current *cleanupState
	Legacy  *legacyCleanupState
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
	return append(body, '\n'), nil
}

func readCleanupState(metadata mergeMetadata, expectedHead string) (cleanupJournal, bool, error) {
	path := cleanupJournalPath(metadata)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return cleanupJournal{}, false, nil
	}
	if err != nil {
		return cleanupJournal{}, false, fmt.Errorf("inspect cleanup state: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return cleanupJournal{}, false, errors.New("cleanup state is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return cleanupJournal{}, false, errors.New("cleanup state permissions are too broad")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return cleanupJournal{}, false, fmt.Errorf("read cleanup state: %w", err)
	}
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return cleanupJournal{}, false, fmt.Errorf("decode cleanup state version: %w", err)
	}
	switch envelope.Version {
	case cleanupStateVersion:
		var state cleanupState
		if err := decodeCleanupJSON(body, &state); err != nil {
			return cleanupJournal{}, false, err
		}
		if err := validateCleanupState(metadata, expectedHead, state); err != nil {
			return cleanupJournal{}, false, err
		}
		return cleanupJournal{Current: &state}, true, nil
	case legacyCleanupStateVersion:
		var state legacyCleanupState
		if err := decodeCleanupJSON(body, &state); err != nil {
			return cleanupJournal{}, false, err
		}
		if err := validateLegacyCleanupState(metadata, expectedHead, state); err != nil {
			return cleanupJournal{}, false, err
		}
		return cleanupJournal{Legacy: &state}, true, nil
	default:
		return cleanupJournal{}, false, fmt.Errorf("unsupported cleanup state version %d", envelope.Version)
	}
}

func decodeCleanupJSON(body []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode cleanup state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("decode cleanup state: trailing JSON content")
	}
	return nil
}

func validateCleanupState(metadata mergeMetadata, expectedHead string, state cleanupState) error {
	if state.Version != cleanupStateVersion {
		return fmt.Errorf("unsupported cleanup state version %d", state.Version)
	}
	if !sameCleanupPath(state.OriginalRoot, metadata.WorktreeRoot) ||
		state.WorktreeBranch != metadata.WorktreeBranch || state.WorktreeHead != expectedHead {
		return errors.New("cleanup state identity does not match the merge receipt")
	}
	if state.Stage != cleanupStagePlanned && state.Stage != cleanupStageRetained {
		return fmt.Errorf("unsupported cleanup stage %q", state.Stage)
	}
	return validateCleanupRecoveryPath(metadata, state.RecoveryRoot)
}

func validateLegacyCleanupState(metadata mergeMetadata, expectedHead string, state legacyCleanupState) error {
	if state.Version != legacyCleanupStateVersion {
		return fmt.Errorf("unsupported cleanup state version %d", state.Version)
	}
	if !sameCleanupPath(state.OriginalRoot, metadata.WorktreeRoot) ||
		state.WorktreeBranch != metadata.WorktreeBranch || state.WorktreeHead != expectedHead {
		return errors.New("cleanup state identity does not match the merge receipt")
	}
	if state.Stage != legacyCleanupStagePrepared && state.Stage != legacyCleanupStageUnregistered {
		return fmt.Errorf("unsupported cleanup stage %q", state.Stage)
	}
	if err := validateCleanupRecoveryPath(metadata, state.RegisteredRoot); err != nil {
		return err
	}
	if err := validateCleanupRecoveryPath(metadata, state.DetachedRoot); err != nil {
		return err
	}
	if sameCleanupPath(state.RegisteredRoot, state.DetachedRoot) {
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

func validateCleanupRecoveryPath(metadata mergeMetadata, path string) error {
	cleanupDir := filepath.Join(filepath.Dir(metadata.WorktreeRoot), ".reasonix-cleanup")
	cleanupInfo, err := os.Lstat(cleanupDir)
	if err != nil || !cleanupInfo.IsDir() || cleanupInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("cleanup recovery directory is not a real directory")
	}
	realCleanupDir, err := filepath.EvalSymlinks(cleanupDir)
	if err != nil {
		return errors.New("cleanup recovery directory cannot be resolved")
	}
	realPath, err := resolveMissingCleanupPath(path)
	if err != nil {
		return errors.New("cleanup recovery path cannot be resolved")
	}
	rel, err := filepath.Rel(filepath.Clean(realCleanupDir), filepath.Clean(realPath))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)) {
		return errors.New("cleanup state path escapes the allocation recovery directory")
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
			item.Digest, err = digestCleanupFile(ctx, path)
		case info.Mode()&os.ModeSymlink != 0:
			var target string
			target, err = os.Readlink(path)
			if err == nil {
				digest := sha256.Sum256([]byte(target))
				item.Digest = hex.EncodeToString(digest[:])
			}
		default:
			err = fmt.Errorf("cleanup path %q has unsupported type %s", relative, info.Mode().Type())
		}
		if err != nil {
			return err
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
			return hex.EncodeToString(hash.Sum(nil)), nil
		}
		if readErr != nil {
			return "", readErr
		}
	}
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
