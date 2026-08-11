package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/desktop/internal/browseripc"
	"reasonix/desktop/internal/update"
	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

var browserComponentInstallMu sync.Mutex

const (
	maxBrowserComponentArchiveBytes = int64(512 << 20)
	maxBrowserComponentExtractBytes = int64(1536 << 20)
	maxBrowserComponentFiles        = 20_000
)

type browserComponentMetadata struct {
	Format          string `json:"format"`
	Version         string `json:"version"`
	ElectronVersion string `json:"electronVersion"`
	ProtocolVersion int    `json:"protocolVersion"`
}

func (a *App) installOrRepairBrowserComponent(ctx context.Context) error {
	browserComponentInstallMu.Lock()
	defer browserComponentInstallMu.Unlock()
	selected := configuredUpdateChannel()
	c, err := httpClient()
	if err != nil {
		return err
	}
	v4, _ := httpClientIPv4()
	manifest, err := fetchManifest(ctx, c, v4, selected)
	if err != nil {
		return fmt.Errorf("load signed browser component manifest: %w", err)
	}
	asset, ok := manifest.BrowserComponent()
	if !ok {
		return fmt.Errorf("browser component is not published for %s", update.CurrentPlatform())
	}
	if asset.Size <= 0 || asset.Size > maxBrowserComponentArchiveBytes {
		return fmt.Errorf("browser component archive size %d is invalid", asset.Size)
	}
	data, err := downloadForChannel(ctx, c, v4, selected, asset.URL, asset.Size, nil)
	if err != nil {
		return fmt.Errorf("download browser component: %w", err)
	}
	sig, err := fetchBytesFallbackForChannelSized(ctx, c, v4, selected, asset.Sig, maxDesktopSignatureSize)
	if err != nil {
		return fmt.Errorf("download browser component signature: %w", err)
	}
	if err := update.Verify(data, sig); err != nil {
		return fmt.Errorf("verify browser component signature: %w", err)
	}
	if err := checkSHA256(data, asset.SHA256); err != nil {
		return fmt.Errorf("verify browser component digest: %w", err)
	}
	if err := installBrowserComponentArchive(data, asset.URL, config.ReasonixHomeDir(), runtime.GOOS); err != nil {
		return err
	}
	if a.browser != nil {
		a.browser.ResetRecovery()
	}
	return nil
}

func installBrowserComponentArchive(data []byte, archiveName, home, goos string) error {
	componentDir := filepath.Join(home, browserComponentDirName)
	if err := os.MkdirAll(componentDir, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(componentDir, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if strings.HasSuffix(strings.ToLower(archiveName), ".zip") {
		err = extractBrowserComponentZIP(data, stage)
	} else if strings.HasSuffix(strings.ToLower(archiveName), ".tar.gz") || strings.HasSuffix(strings.ToLower(archiveName), ".tgz") {
		err = extractBrowserComponentTarGZ(data, stage)
	} else {
		return fmt.Errorf("unsupported browser component archive %q", filepath.Base(archiveName))
	}
	if err != nil {
		return fmt.Errorf("extract browser component: %w", err)
	}
	var current struct {
		Version string `json:"version"`
	}
	currentRaw, err := readRegularComponentFile(filepath.Join(stage, browserCurrentManifest), 64<<10)
	if err != nil || json.Unmarshal(currentRaw, &current) != nil || !validComponentVersion(current.Version) {
		return fmt.Errorf("browser component has an invalid current manifest")
	}
	versionDir := filepath.Join(stage, current.Version)
	if err := validateComponentVersionTree(versionDir); err != nil {
		return fmt.Errorf("browser component version tree is invalid: %w", err)
	}
	var metadata browserComponentMetadata
	metaRaw, err := readRegularComponentFile(filepath.Join(versionDir, "component.json"), 64<<10)
	if err != nil || json.Unmarshal(metaRaw, &metadata) != nil ||
		metadata.Format != "reasonix.browser.component.v1" || metadata.Version != current.Version ||
		!validComponentVersion(metadata.ElectronVersion) || metadata.ProtocolVersion != browseripc.ProtocolVersion {
		return fmt.Errorf("browser component metadata is incompatible")
	}
	binary := filepath.Join(versionDir, browserComponentBinaryDir, browserComponentBinaryNameFor(goos))
	if st, err := os.Stat(binary); err != nil || st.IsDir() {
		return fmt.Errorf("browser component executable is missing")
	}

	target := filepath.Join(componentDir, current.Version)
	backup := target + fmt.Sprintf(".repair-%d", time.Now().UnixNano())
	hadTarget := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("stage existing browser component: %w", err)
		}
		hadTarget = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(versionDir, target); err != nil {
		if hadTarget {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("install browser component: %w", err)
	}
	manifestData, _ := json.MarshalIndent(current, "", "  ")
	tmpManifest := filepath.Join(componentDir, browserCurrentManifest+".tmp")
	if err := os.WriteFile(tmpManifest, append(manifestData, '\n'), 0o644); err != nil {
		rollbackBrowserComponent(target, backup, hadTarget)
		return err
	}
	if err := fileutil.ReplaceFile(tmpManifest, filepath.Join(componentDir, browserCurrentManifest)); err != nil {
		rollbackBrowserComponent(target, backup, hadTarget)
		return err
	}
	if hadTarget {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func rollbackBrowserComponent(target, backup string, hadTarget bool) {
	_ = os.RemoveAll(target)
	if hadTarget {
		_ = os.Rename(backup, target)
	}
}

func extractBrowserComponentZIP(data []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	entries := make([]componentArchiveEntry, 0, len(zr.File))
	for _, f := range zr.File {
		entry, err := componentZIPEntry(f)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
	}
	if err := validateComponentArchivePlan(entries); err != nil {
		return err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	for i, f := range zr.File {
		entry := entries[i]
		switch entry.kind {
		case componentArchiveDirectory:
			if err := root.MkdirAll(entry.name, 0o755); err != nil {
				return err
			}
		case componentArchiveFile:
			r, err := f.Open()
			if err != nil {
				return err
			}
			err = extractComponentFile(root, entry, r)
			r.Close()
			if err != nil {
				return err
			}
		}
	}
	return createComponentArchiveSymlinks(root, entries)
}

func extractBrowserComponentTarGZ(data []byte, dest string) error {
	entries, err := scanBrowserComponentTarGZ(data)
	if err != nil {
		return err
	}
	if err := validateComponentArchivePlan(entries); err != nil {
		return err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	index := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if index >= len(entries) {
			return fmt.Errorf("archive changed while extracting")
		}
		entry := entries[index]
		index++
		switch entry.kind {
		case componentArchiveDirectory:
			if err := root.MkdirAll(entry.name, 0o755); err != nil {
				return err
			}
		case componentArchiveFile:
			if err := extractComponentFile(root, entry, io.LimitReader(tr, h.Size)); err != nil {
				return err
			}
		}
	}
	if index != len(entries) {
		return fmt.Errorf("archive changed while extracting")
	}
	return createComponentArchiveSymlinks(root, entries)
}

type componentArchiveEntryKind uint8

const (
	componentArchiveFile componentArchiveEntryKind = iota
	componentArchiveDirectory
	componentArchiveSymlink
)

type componentArchiveEntry struct {
	name string
	kind componentArchiveEntryKind
	mode os.FileMode
	size uint64
	link string
}

func componentZIPEntry(f *zip.File) (componentArchiveEntry, error) {
	name, err := normalizeComponentArchivePath(f.Name)
	if err != nil {
		return componentArchiveEntry{}, err
	}
	mode := f.Mode()
	entry := componentArchiveEntry{name: name, mode: mode, size: f.UncompressedSize64}
	switch {
	case f.FileInfo().IsDir():
		entry.kind = componentArchiveDirectory
		entry.size = 0
	case mode&os.ModeSymlink != 0:
		if f.UncompressedSize64 > 4096 {
			return componentArchiveEntry{}, fmt.Errorf("invalid symlink %q", f.Name)
		}
		r, err := f.Open()
		if err != nil {
			return componentArchiveEntry{}, err
		}
		link, readErr := io.ReadAll(io.LimitReader(r, 4097))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || len(link) > 4096 {
			return componentArchiveEntry{}, fmt.Errorf("invalid symlink %q", f.Name)
		}
		entry.kind = componentArchiveSymlink
		entry.size = 0
		entry.link, err = normalizeComponentSymlink(name, string(link))
		if err != nil {
			return componentArchiveEntry{}, err
		}
	case mode.Type() == 0:
		entry.kind = componentArchiveFile
	default:
		return componentArchiveEntry{}, fmt.Errorf("unsupported zip entry %q", f.Name)
	}
	return entry, nil
}

func scanBrowserComponentTarGZ(data []byte) ([]componentArchiveEntry, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var entries []componentArchiveEntry
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		if h.Size < 0 {
			return nil, fmt.Errorf("archive entry %q has a negative size", h.Name)
		}
		name, err := normalizeComponentArchivePath(h.Name)
		if err != nil {
			return nil, err
		}
		entry := componentArchiveEntry{name: name, mode: os.FileMode(h.Mode) & 0o777, size: uint64(h.Size)}
		switch h.Typeflag {
		case tar.TypeReg:
			entry.kind = componentArchiveFile
		case tar.TypeDir:
			entry.kind = componentArchiveDirectory
			entry.size = 0
		case tar.TypeSymlink:
			entry.kind = componentArchiveSymlink
			entry.size = 0
			entry.link, err = normalizeComponentSymlink(name, h.Linkname)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported tar entry %q", h.Name)
		}
		entries = append(entries, entry)
	}
}

func validateComponentArchivePlan(entries []componentArchiveEntry) error {
	if len(entries) > maxBrowserComponentFiles {
		return fmt.Errorf("archive contains too many files")
	}
	kinds := make(map[string]componentArchiveEntryKind, len(entries))
	var extracted int64
	for _, entry := range entries {
		if _, exists := kinds[entry.name]; exists {
			return fmt.Errorf("duplicate archive path %q", entry.name)
		}
		kinds[entry.name] = entry.kind
		if entry.kind == componentArchiveFile {
			if entry.size > uint64(maxBrowserComponentExtractBytes) || extracted > maxBrowserComponentExtractBytes-int64(entry.size) {
				return fmt.Errorf("archive exceeds extracted byte budget")
			}
			extracted += int64(entry.size)
		}
	}
	for _, entry := range entries {
		for parent := path.Dir(entry.name); parent != "."; parent = path.Dir(parent) {
			if kind, exists := kinds[parent]; exists && kind != componentArchiveDirectory {
				return fmt.Errorf("archive path %q has a non-directory parent", entry.name)
			}
		}
	}
	return nil
}

func extractComponentFile(root *os.Root, entry componentArchiveEntry, r io.Reader) error {
	if err := root.MkdirAll(path.Dir(entry.name), 0o755); err != nil {
		return err
	}
	perm := entry.mode.Perm()
	if perm == 0 {
		perm = 0o644
	}
	f, err := root.OpenFile(entry.name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(r, int64(entry.size)+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != int64(entry.size) {
		return fmt.Errorf("archive entry %q size mismatch", entry.name)
	}
	return nil
}

func normalizeComponentArchivePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := strings.TrimPrefix(path.Clean(name), "./")
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(name) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.ContainsRune(clean, 0) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func normalizeComponentSymlink(name, link string) (string, error) {
	link = strings.ReplaceAll(link, "\\", "/")
	if link == "" || path.IsAbs(link) || filepath.IsAbs(link) || filepath.VolumeName(link) != "" || strings.ContainsRune(link, 0) {
		return "", fmt.Errorf("absolute symlink %q", name)
	}
	clean := path.Clean(link)
	resolved := path.Clean(path.Join(path.Dir(name), clean))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", fmt.Errorf("symlink escapes destination: %q", name)
	}
	return filepath.FromSlash(clean), nil
}

func createComponentArchiveSymlinks(root *os.Root, entries []componentArchiveEntry) error {
	// Links are created only after every regular file and directory. An archive
	// can therefore never make a later file write follow an archive-controlled
	// link, while macOS Electron Framework links remain intact.
	for _, entry := range entries {
		if entry.kind != componentArchiveSymlink {
			continue
		}
		if err := root.MkdirAll(path.Dir(entry.name), 0o755); err != nil {
			return err
		}
		if err := root.Symlink(entry.link, entry.name); err != nil {
			return err
		}
	}
	return nil
}

func readRegularComponentFile(name string, maxBytes int64) ([]byte, error) {
	st, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() < 0 || st.Size() > maxBytes {
		return nil, fmt.Errorf("%s is not a bounded regular file", filepath.Base(name))
	}
	return os.ReadFile(name)
}

func validateComponentVersionTree(versionDir string) error {
	root, err := filepath.EvalSymlinks(versionDir)
	if err != nil {
		return err
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("version root is not a directory")
	}
	return filepath.WalkDir(versionDir, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(name)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", filepath.Base(name), err)
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("symlink %s leaves the version directory", filepath.Base(name))
		}
		return nil
	})
}

func validComponentVersion(v string) bool {
	if v == "" || len(v) > 128 || v == "." || v == ".." {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '.' && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
