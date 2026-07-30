package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/fileutil"
)

func themeReplaceFile(tmp, dest string) error {
	return fileutil.ReplaceFile(tmp, dest)
}

// importThemePackZIP validates and extracts a .reasonix-theme ZIP into a staging dir.
// The caller must publish with publishThemeDir.
func importThemePackZIP(zipPath string) (manifest *ThemePackManifest, staging string, err error) {
	info, err := os.Lstat(zipPath)
	if err != nil {
		return nil, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("theme package must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("theme package must be a regular file")
	}
	if info.Size() > themePackMaxZipBytes {
		return nil, "", fmt.Errorf("theme package exceeds %d bytes", themePackMaxZipBytes)
	}

	f, err := os.Open(zipPath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	// Re-check size after open (TOCTOU).
	fi, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if fi.Size() > themePackMaxZipBytes {
		return nil, "", fmt.Errorf("theme package exceeds %d bytes", themePackMaxZipBytes)
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, "", fmt.Errorf("invalid theme ZIP: %w", err)
	}
	return extractThemeZip(zr)
}

func extractThemeZip(zr *zip.Reader) (*ThemePackManifest, string, error) {
	seen := map[string]struct{}{}
	var manifestEntry *zip.File
	imageEntries := map[string]*zip.File{}

	for _, zf := range zr.File {
		name := sanitizeZipEntryName(zf.Name)
		if name == "" {
			return nil, "", fmt.Errorf("theme package contains an empty or unsafe path")
		}
		if strings.Contains(name, "/") || strings.Contains(name, `\`) {
			return nil, "", fmt.Errorf("theme package may only contain root-level files (got %q)", zf.Name)
		}
		if _, dup := seen[strings.ToLower(name)]; dup {
			return nil, "", fmt.Errorf("theme package contains duplicate entry %q", name)
		}
		seen[strings.ToLower(name)] = struct{}{}

		if zf.FileInfo().IsDir() {
			return nil, "", fmt.Errorf("theme package must not contain directories")
		}
		// Detect symlink-like mode bits when present.
		if zf.Mode()&os.ModeSymlink != 0 {
			return nil, "", fmt.Errorf("theme package must not contain symlinks")
		}
		if zf.UncompressedSize64 > themePackMaxImageBytes && !strings.EqualFold(name, themePackManifestName) {
			return nil, "", fmt.Errorf("theme package entry %q is too large", name)
		}
		if zf.UncompressedSize64 > themePackMaxManifest && strings.EqualFold(name, themePackManifestName) {
			return nil, "", fmt.Errorf("theme manifest is too large")
		}

		if strings.EqualFold(name, themePackManifestName) {
			manifestEntry = zf
			continue
		}
		if themePackImageRe.MatchString(name) {
			if len(imageEntries) >= 2 {
				return nil, "", fmt.Errorf("theme package may contain at most two scene images")
			}
			imageEntries[strings.ToLower(name)] = zf
			continue
		}
		return nil, "", fmt.Errorf("theme package contains disallowed file %q", name)
	}

	if manifestEntry == nil {
		return nil, "", fmt.Errorf("theme package missing %s", themePackManifestName)
	}

	raw, err := readZipFileLimited(manifestEntry, themePackMaxManifest)
	if err != nil {
		return nil, "", err
	}
	m, err := parseThemePackManifest(raw)
	if err != nil {
		return nil, "", err
	}
	if isReservedThemeID(m.ID) {
		return nil, "", fmt.Errorf("cannot import over built-in theme id %q", m.ID)
	}

	expectedImages := map[string]string{}
	if m.Background != nil && m.Background.Image != "" {
		expectedImages[strings.ToLower(m.Background.Image)] = m.Background.Image
	}
	if m.TaskBackground != nil && m.TaskBackground.Image != "" {
		expectedImages[strings.ToLower(m.TaskBackground.Image)] = m.TaskBackground.Image
	}
	for key, manifestName := range expectedImages {
		if imageEntries[key] == nil {
			return nil, "", fmt.Errorf("manifest references scene image %q but ZIP has none", manifestName)
		}
	}
	for key := range imageEntries {
		if _, ok := expectedImages[key]; !ok {
			return nil, "", fmt.Errorf("ZIP contains scene image not referenced by manifest")
		}
	}

	staging, err := os.MkdirTemp("", "reasonix-theme-import-*")
	if err != nil {
		return nil, "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()

	// Write manifest as canonical JSON from validated struct.
	canonical, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(filepath.Join(staging, themePackManifestName), canonical, 0o644); err != nil {
		return nil, "", err
	}

	for key, manifestName := range expectedImages {
		imgData, err := readZipFileLimited(imageEntries[key], themePackMaxImageBytes)
		if err != nil {
			return nil, "", err
		}
		imgPath := filepath.Join(staging, manifestName)
		if err := os.WriteFile(imgPath, imgData, 0o644); err != nil {
			return nil, "", err
		}
		if err := validateThemeImageFile(imgPath); err != nil {
			return nil, "", err
		}
	}

	cleanup = false
	return m, staging, nil
}

func sanitizeZipEntryName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, `\`, "/")
	// Drop absolute and parent paths (ZIP slip).
	name = strings.TrimPrefix(name, "/")
	for strings.HasPrefix(name, "../") || name == ".." {
		return ""
	}
	if strings.Contains(name, "/../") || strings.HasSuffix(name, "/..") {
		return ""
	}
	// Only basename — reject nested paths elsewhere.
	if i := strings.LastIndex(name, "/"); i >= 0 {
		// Nested path — return as-is so caller rejects.
		return name
	}
	if name == "." || name == ".." {
		return ""
	}
	return name
}

func readZipFileLimited(zf *zip.File, max int64) ([]byte, error) {
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("ZIP entry %q exceeds size limit", zf.Name)
	}
	return data, nil
}

// exportThemePackZIP writes a validated user theme to a ZIP path.
// Reserved ids (base styles + official themes) are refused: an exported pack
// could never be re-imported because the id is reserved. Duplicate first.
func exportThemePackZIP(id, destPath string) error {
	id = strings.TrimSpace(id)
	if isReservedThemeID(id) {
		return fmt.Errorf("built-in themes cannot be exported; create a copy first")
	}
	m, err := loadUserThemeManifest(id)
	if err != nil {
		return err
	}
	var img []byte
	var taskImg []byte
	if m.Background != nil && m.Background.Image != "" {
		imgPath, err := resolveThemeImageAbs(id, m.Background.Image)
		if err != nil {
			return err
		}
		img, err = os.ReadFile(imgPath)
		if err != nil {
			return err
		}
	}
	if m.TaskBackground != nil && m.TaskBackground.Image != "" {
		imgPath, err := resolveThemeImageAbs(id, m.TaskBackground.Image)
		if err != nil {
			return err
		}
		taskImg, err = os.ReadFile(imgPath)
		if err != nil {
			return err
		}
	}
	return writeThemeZip(destPath, m, img, taskImg)
}

func findBuiltinManifest(id string) *ThemePackManifest {
	for _, m := range builtinThemePacks() {
		if m.ID == id {
			cp := m
			return &cp
		}
	}
	return nil
}

func writeThemeZip(destPath string, m *ThemePackManifest, imageBytes []byte, taskImageBytes ...[]byte) error {
	if err := validateThemePackManifest(m); err != nil {
		return err
	}
	destPath = filepath.Clean(destPath)
	if !strings.HasSuffix(strings.ToLower(destPath), themePackExt) {
		destPath += themePackExt
	}
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".export-theme-*.zip")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	zw := zip.NewWriter(tmp)
	manifestBytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	w, err := zw.Create(themePackManifestName)
	if err != nil {
		return err
	}
	if _, err := w.Write(manifestBytes); err != nil {
		return err
	}
	if m.Background != nil && m.Background.Image != "" {
		if len(imageBytes) == 0 {
			return fmt.Errorf("missing background image bytes")
		}
		iw, err := zw.Create(m.Background.Image)
		if err != nil {
			return err
		}
		if _, err := iw.Write(imageBytes); err != nil {
			return err
		}
	}
	if m.TaskBackground != nil && m.TaskBackground.Image != "" {
		var data []byte
		if len(taskImageBytes) > 0 {
			data = taskImageBytes[0]
		}
		if len(data) == 0 {
			return fmt.Errorf("missing task background image bytes")
		}
		iw, err := zw.Create(m.TaskBackground.Image)
		if err != nil {
			return err
		}
		if _, err := iw.Write(data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// ReplaceFile retries Windows AV/indexer locks and falls back on EXDEV.
	if err := themeReplaceFile(tmpName, destPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// extractThemeZipBytes is a test helper for in-memory ZIP fixtures.
func extractThemeZipBytes(data []byte) (*ThemePackManifest, string, error) {
	if int64(len(data)) > themePackMaxZipBytes {
		return nil, "", fmt.Errorf("theme package exceeds %d bytes", themePackMaxZipBytes)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", err
	}
	return extractThemeZip(zr)
}
