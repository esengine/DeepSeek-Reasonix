package theme

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// ErrNotAPack is an upload the kernel cannot install: no manifest, a manifest
// the reader refuses, or a name no pack may have. The user can fix it; a disk
// that would not take the files is a different answer.
var ErrNotAPack = errors.New("theme: not an installable pack")

const (
	maxManifestBytes = 1 << 20
	maxArchiveBytes  = 40 << 20
	stagingPrefix    = ".import-"
)

// Installed is what an import put on disk, and what it left behind unread.
type Installed struct {
	Pack    Pack     `json:"pack"`
	Ignored []string `json:"ignored,omitempty"`
}

// InstallArchive installs the pack a zip holds. The manifest may sit at the
// root or inside one folder; only that folder's files are read.
func InstallArchive(raw []byte, hint string) (Installed, error) {
	if len(raw) > maxArchiveBytes {
		return Installed{}, fmt.Errorf("%w: archive is %d bytes", ErrNotAPack, len(raw))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return Installed{}, fmt.Errorf("%w: %w", ErrNotAPack, err)
	}
	root := ""
	found := 0
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, `\`, "/"))
		if strings.EqualFold(path.Base(name), manifestName) && !strings.HasPrefix(name, "__MACOSX/") {
			root = path.Dir(name)
			found++
		}
	}
	if found != 1 {
		return Installed{}, fmt.Errorf("%w: the archive holds %d %s files, want 1", ErrNotAPack, found, manifestName)
	}
	files := map[string][]byte{}
	var budget int64 = maxArchiveBytes
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, `\`, "/"))
		if f.FileInfo().IsDir() || path.Dir(name) != root {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Installed{}, fmt.Errorf("%w: %w", ErrNotAPack, err)
		}
		// The declared size is the archive's claim; the limit is on what is read.
		body, err := io.ReadAll(io.LimitReader(rc, min(budget, maxAssetBytes)+1))
		rc.Close()
		if err != nil {
			return Installed{}, fmt.Errorf("%w: %w", ErrNotAPack, err)
		}
		if budget -= int64(len(body)); budget < 0 {
			return Installed{}, fmt.Errorf("%w: the archive unpacks past %d bytes", ErrNotAPack, maxArchiveBytes)
		}
		files[path.Base(name)] = body
	}
	if root != "." {
		hint = path.Base(root)
	}
	return Install(files, hint)
}

// Install writes a pack from loose files into Dir(). Only the manifest and
// the two images a pack may carry are kept; everything else is named back in
// Ignored. An installed pack of the same id is replaced.
func Install(files map[string][]byte, hint string) (Installed, error) {
	keep := map[string][]byte{}
	var ignored []string
	for name, body := range files {
		base := path.Base(strings.ReplaceAll(name, `\`, "/"))
		target, ok := installName(base)
		if !ok || keep[target] != nil {
			ignored = append(ignored, base)
			continue
		}
		limit := maxAssetBytes
		if target == manifestName {
			limit = maxManifestBytes
		}
		if len(body) > limit {
			return Installed{}, fmt.Errorf("%w: %s is %d bytes", ErrNotAPack, base, len(body))
		}
		keep[target] = body
	}
	slices.Sort(ignored)
	manifestRaw := keep[manifestName]
	if manifestRaw == nil {
		return Installed{}, fmt.Errorf("%w: no %s", ErrNotAPack, manifestName)
	}
	id, err := installID(manifestRaw, hint)
	if err != nil {
		return Installed{}, err
	}
	if _, err := decode(manifestRaw, id); err != nil {
		return Installed{}, fmt.Errorf("%w: %w", ErrNotAPack, err)
	}
	if err := stagePack(id, keep); err != nil {
		return Installed{}, err
	}
	pack, err := Load(id)
	if err != nil {
		return Installed{}, err
	}
	return Installed{Pack: pack, Ignored: ignored}, nil
}

// installName maps an uploaded file onto the name the reader looks for, or
// refuses it. One image per kind: the first extension Asset tries wins.
func installName(base string) (string, bool) {
	lower := strings.ToLower(base)
	if lower == manifestName {
		return manifestName, true
	}
	for _, kind := range []Kind{assetBackground, assetPreview} {
		for _, ext := range extensions {
			if lower == string(kind)+ext {
				return string(kind) + ext, true
			}
		}
	}
	return "", false
}

// installID prefers the id the manifest declares, then the folder or file the
// pack came in. The directory name is the id every later read uses.
func installID(manifestRaw []byte, hint string) (string, error) {
	var m manifest
	_ = json.Unmarshal(manifestRaw, &m)
	for _, candidate := range []string{m.ID, strings.TrimSuffix(strings.TrimSuffix(hint, ".zip"), ".json")} {
		id := strings.TrimSpace(candidate)
		if installableID(id) {
			return id, nil
		}
	}
	return "", fmt.Errorf("%w: the manifest has no usable id", ErrNotAPack)
}

// installableID refuses what validID lets through and a directory must not be
// named: the dot entries, a hidden staging name, and the plugin namespace,
// which only an enabled plugin writes.
func installableID(id string) bool {
	if validID(id) != nil || id == manifestName {
		return false
	}
	return !strings.HasPrefix(id, ".") && !isPluginID(id) && !strings.ContainsAny(id, `:*?"<>|`)
}

// stagePack stages the files beside the target and swaps them in, so a failed
// write leaves the previous pack whole. List skips the dot-named staging dirs.
func stagePack(id string, files map[string][]byte) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dir, stagingPrefix)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(stage, name), body, 0o644); err != nil {
			return err
		}
	}
	target := filepath.Join(dir, id)
	old := ""
	if _, err := os.Stat(target); err == nil {
		old = stage + ".old"
		if err := os.Rename(target, old); err != nil {
			return err
		}
	}
	if err := os.Rename(stage, target); err != nil {
		if old != "" {
			_ = os.Rename(old, target)
		}
		return err
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return nil
}
