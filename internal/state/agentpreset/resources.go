package agentpreset

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/agentcomposition"
)

const maxAssetBytes = 128 << 20
const maxAssetFiles = 20000

type AssetFile struct {
	Path string `json:"path"`
	Data []byte `json:"data,omitempty"`
	Mode uint32 `json:"mode,omitempty"`
	Link string `json:"link,omitempty"`
}

type DirectorySnapshot struct {
	Files []AssetFile `json:"files"`
}

// CaptureDirectory retains executable bits without retaining mutable ownership.
func CaptureDirectory(root string) (DirectorySnapshot, error) {
	snapshot := DirectorySnapshot{Files: []AssetFile{}}
	size := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == ".git" || entry.Name() == ".hg" || entry.Name() == ".svn" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if secrets.SensitiveFileName(entry.Name()) {
			return fmt.Errorf("%w: resource requires an external credential reference", agentcomposition.ErrUnavailable)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%w: unsupported resource file %s", agentcomposition.ErrUnavailable, path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxAssetBytes-int64(size) || len(snapshot.Files) >= maxAssetFiles {
			return fmt.Errorf("%w: resource limit exceeded", agentcomposition.ErrUnavailable)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		size += len(data)
		snapshot.Files = append(snapshot.Files, AssetFile{Path: filepath.ToSlash(rel), Data: data, Mode: uint32(info.Mode().Perm() & 0o700)})
		return nil
	})
	return snapshot, err
}

func validateAssets(snapshot DirectorySnapshot) error {
	seen := map[string]bool{}
	size := 0
	for _, f := range snapshot.Files {
		path := filepath.FromSlash(f.Path)
		if !filepath.IsLocal(path) || filepath.Clean(path) != path || f.Link != "" || seen[path] || f.Mode & ^uint32(0o700) != 0 {
			return fmt.Errorf("%w: unsafe resource path %s", agentcomposition.ErrInvalid, f.Path)
		}
		seen[path] = true
		size += len(f.Data)
		if size > maxAssetBytes || len(snapshot.Files) > maxAssetFiles {
			return fmt.Errorf("%w: resource limit exceeded", agentcomposition.ErrInvalid)
		}
	}
	return nil
}

// Materialize verifies existing retained files before reusing their directory.
func (s *Store) Materialize(snapshot DirectorySnapshot) (string, error) {
	if err := validateAssets(snapshot); err != nil {
		return "", err
	}
	snapshot = agentcomposition.Clone(snapshot)
	for i := range snapshot.Files {
		snapshot.Files[i].Mode |= 0o600
	}
	sort.Slice(snapshot.Files, func(i, j int) bool { return snapshot.Files[i].Path < snapshot.Files[j].Path })
	digest := agentcomposition.Digest(snapshot)
	root := filepath.Join(s.trustedDirectory(), "resources", digest)
	unlock, err := lock(root)
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err := os.Lstat(root); err == nil {
		existing, err := CaptureDirectory(root)
		if err != nil {
			return "", err
		}
		if agentcomposition.Digest(existing) != digest {
			return "", fmt.Errorf("%w: retained resource changed", agentcomposition.ErrUnavailable)
		}
		return root, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temp, err := os.MkdirTemp(filepath.Dir(root), ".resource-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	for _, f := range snapshot.Files {
		path := filepath.Join(temp, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, f.Data, os.FileMode(f.Mode)|0o600); err != nil {
			return "", err
		}
	}
	if err := os.Rename(temp, root); err != nil {
		return "", err
	}
	return root, nil
}
