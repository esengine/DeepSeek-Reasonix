package worktree

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

const snapshotMetadataMaxBytes = 8 << 20

type snapshotMetadata struct {
	Version     int               `json:"version"`
	Files       map[string]uint32 `json:"files"`
	Directories map[string]uint32 `json:"directories"`
}

func captureSnapshotMetadata(ctx context.Context, repo gitcmd.Repo, tree string) (string, error) {
	entries, err := snapshotEntries(ctx, repo, tree)
	if err != nil {
		return "", err
	}
	trust, err := readWorktreeTrust(ctx, repo)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(repo.WorkTree)
	if err != nil {
		return "", err
	}
	defer root.Close()
	metadata := snapshotMetadata{Version: 1, Files: map[string]uint32{}, Directories: map[string]uint32{}}
	for _, entry := range append(entries, snapshotEntry{Path: ".", Mode: "160000"}) {
		path := entry.Path
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := plainEnvironmentParents(root, path); err != nil {
			return "", err
		}
		info, err := root.Lstat(path)
		if err != nil {
			return "", err
		}
		switch entry.Mode {
		case "100644", "100755":
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("snapshot file identity changed: %s", path)
			}
			metadata.Files[path] = uint32(info.Mode().Perm())
		case "160000":
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("snapshot gitlink identity changed: %s", path)
			}
			metadata.Directories[path] = uint32(info.Mode().Perm())
		case "120000":
			if info.Mode()&os.ModeSymlink == 0 && (trust.symlinks || !info.Mode().IsRegular()) {
				return "", fmt.Errorf("snapshot symlink identity changed: %s", path)
			}
		}
		for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
			info, err := root.Lstat(parent)
			if err != nil {
				return "", err
			}
			metadata.Directories[parent] = uint32(info.Mode().Perm())
		}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	if len(raw) > snapshotMetadataMaxBytes {
		return "", fmt.Errorf("snapshot metadata exceeds supported byte limit")
	}
	oid, stderr, err := runGitInput(ctx, repo, string(raw), "hash-object", "-w", "--stdin")
	if err != nil {
		return "", fmt.Errorf("record snapshot modes: %w%s", err, stderrSuffix(stderr))
	}
	return strings.TrimSpace(oid), nil
}

func applySnapshotMetadata(ctx context.Context, snap Snapshot, candidate Candidate) error {
	if snap.Metadata == "" {
		info, err := os.Stat(snap.RepoRoot)
		if err != nil {
			return err
		}
		return os.Chmod(candidate.WorktreeRoot, info.Mode().Perm())
	}
	metadata, err := readSnapshotMetadata(ctx, snap)
	if err != nil {
		return err
	}
	repo, err := snap.repo(ctx)
	if err != nil {
		return err
	}
	entries, err := snapshotEntries(ctx, repo, snap.Tree)
	if err != nil {
		return err
	}
	return metadata.apply(candidate.WorktreeRoot, entries)
}

func readSnapshotMetadata(ctx context.Context, snap Snapshot) (snapshotMetadata, error) {
	if len(snap.Metadata) != 40 && len(snap.Metadata) != 64 {
		return snapshotMetadata{}, fmt.Errorf("invalid snapshot metadata object")
	}
	if _, err := hex.DecodeString(snap.Metadata); err != nil {
		return snapshotMetadata{}, err
	}
	repo, err := snap.repo(ctx)
	if err != nil {
		return snapshotMetadata{}, err
	}
	size, stderr, err := runGit(ctx, repo, "cat-file", "-s", snap.Metadata)
	if err != nil {
		return snapshotMetadata{}, fmt.Errorf("read snapshot metadata size: %w%s", err, stderrSuffix(stderr))
	}
	n, err := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
	if err != nil || n < 0 || n > snapshotMetadataMaxBytes {
		return snapshotMetadata{}, fmt.Errorf("snapshot metadata exceeds supported byte limit")
	}
	raw, stderr, err := runGit(ctx, repo, "cat-file", "blob", snap.Metadata)
	if err != nil {
		return snapshotMetadata{}, fmt.Errorf("read snapshot modes: %w%s", err, stderrSuffix(stderr))
	}
	var metadata snapshotMetadata
	if len(raw) > snapshotMetadataMaxBytes || json.Unmarshal([]byte(raw), &metadata) != nil || metadata.Version != 1 {
		return snapshotMetadata{}, fmt.Errorf("unsupported snapshot metadata; candidate retained")
	}
	return metadata, nil
}

func (m snapshotMetadata) apply(target string, entries []snapshotEntry) error {
	allowedFiles, allowedDirs := map[string]bool{}, map[string]bool{".": true}
	for _, entry := range entries {
		path := entry.Path
		switch entry.Mode {
		case "100644", "100755":
			allowedFiles[path] = true
		case "160000":
			allowedDirs[path] = true
		}
		for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
			allowedDirs[parent] = true
		}
	}
	if len(m.Files) != len(allowedFiles) || len(m.Directories) != len(allowedDirs) {
		return fmt.Errorf("incomplete snapshot mode metadata")
	}
	for path := range allowedFiles {
		if _, ok := m.Files[path]; !ok {
			return fmt.Errorf("missing snapshot file mode: %s", path)
		}
	}
	for path := range allowedDirs {
		if _, ok := m.Directories[path]; !ok {
			return fmt.Errorf("missing snapshot directory mode: %s", path)
		}
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	for path, mode := range m.Files {
		if !allowedFiles[path] || mode&^0777 != 0 {
			return fmt.Errorf("invalid file mode metadata")
		}
		if err := plainEnvironmentParents(root, path); err != nil {
			return err
		}
		info, err := root.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("snapshot file identity changed: %s", path)
		}
		if err := root.Chmod(path, os.FileMode(mode)); err != nil {
			return err
		}
	}
	dirs := make([]string, 0, len(m.Directories))
	for path := range m.Directories {
		dirs = append(dirs, path)
	}
	slices.Sort(dirs)
	for _, path := range slices.Backward(dirs) {
		if !allowedDirs[path] || m.Directories[path]&^0777 != 0 {
			return fmt.Errorf("invalid directory mode metadata")
		}
		if err := plainEnvironmentParents(root, path); err != nil {
			return err
		}
		info, err := root.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot directory identity changed: %s", path)
		}
		if err := root.Chmod(path, os.FileMode(m.Directories[path])); err != nil {
			return err
		}
	}
	return nil
}
