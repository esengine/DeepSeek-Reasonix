package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

func themeStatePath() string {
	return filepath.Join(config.MemoryUserDir(), themeStateFileName)
}

func themesRootDir() string {
	return filepath.Join(config.MemoryUserDir(), themeDirName)
}

func themeDir(id string) string {
	return filepath.Join(themesRootDir(), id)
}

func themeManifestPath(id string) string {
	return filepath.Join(themeDir(id), themePackManifestName)
}

func loadThemeDesktopState() ThemeDesktopState {
	path := themeStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return ThemeDesktopState{SchemaVersion: themeStateSchemaVer}
	}
	var st ThemeDesktopState
	if err := json.Unmarshal(data, &st); err != nil {
		return ThemeDesktopState{SchemaVersion: themeStateSchemaVer}
	}
	if st.SchemaVersion == 0 {
		st.SchemaVersion = themeStateSchemaVerV1
	}
	st.ActiveThemeID = strings.TrimSpace(st.ActiveThemeID)
	return st
}

func saveThemeDesktopState(st ThemeDesktopState) error {
	st.SchemaVersion = themeStateSchemaVer
	st.ActiveThemeID = strings.TrimSpace(st.ActiveThemeID)
	// Hard rule for v2: never persist base style ids as the active pack.
	if isBuiltinThemeID(st.ActiveThemeID) {
		st.ActiveThemeID = ""
	}
	dir := filepath.Dir(themeStatePath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(themeStatePath(), data, 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	// Use the shared Windows-safe atomic writer (AV/indexer lock retries +
	// cross-device fallback) instead of a bare os.Rename.
	return fileutil.AtomicWriteFile(path, data, mode)
}

func loadUserThemeManifest(id string) (*ThemePackManifest, error) {
	id = strings.TrimSpace(id)
	if !themePackIDRe.MatchString(id) {
		return nil, fmt.Errorf("invalid theme id")
	}
	if isReservedThemeID(id) {
		return nil, fmt.Errorf("built-in theme %q has no user directory", id)
	}
	data, err := os.ReadFile(themeManifestPath(id))
	if err != nil {
		return nil, err
	}
	return parseThemePackManifest(data)
}

func listUserThemeIDs() ([]string, error) {
	root := themesRootDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !themePackIDRe.MatchString(id) || isReservedThemeID(id) {
			continue
		}
		if _, err := os.Stat(themeManifestPath(id)); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func userThemeExists(id string) bool {
	_, err := os.Stat(themeManifestPath(id))
	return err == nil
}

// publishThemeDir atomically replaces themes/<id> with the prepared staging directory.
// stagingDir must already contain a validated theme.json and optional scene images.
func publishThemeDir(id, stagingDir string, replace bool) error {
	id = strings.TrimSpace(id)
	if !themePackIDRe.MatchString(id) {
		return fmt.Errorf("invalid theme id")
	}
	if isReservedThemeID(id) {
		return fmt.Errorf("built-in themes cannot be overwritten")
	}
	dest := themeDir(id)
	if userThemeExists(id) && !replace {
		return fmt.Errorf("theme %q already exists (set replace to overwrite)", id)
	}
	root := themesRootDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	// Final rename target: themes/<id>
	// Strategy: write to themes/.staging-<id>-* then rename over destination.
	parentStaging, err := os.MkdirTemp(root, ".staging-"+id+"-")
	if err != nil {
		return err
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = os.RemoveAll(parentStaging)
		}
	}()

	// Copy staging contents into parentStaging (re-validate containment).
	if err := copyThemeTree(stagingDir, parentStaging); err != nil {
		return err
	}
	// Verify manifest still parses after copy.
	data, err := os.ReadFile(filepath.Join(parentStaging, themePackManifestName))
	if err != nil {
		return err
	}
	m, err := parseThemePackManifest(data)
	if err != nil {
		return err
	}
	if m.ID != id {
		return fmt.Errorf("theme id mismatch: manifest %q vs directory %q", m.ID, id)
	}
	if m.Background != nil && m.Background.Image != "" {
		imgPath := filepath.Join(parentStaging, m.Background.Image)
		if err := validateThemeImageFile(imgPath); err != nil {
			return err
		}
	}
	if m.TaskBackground != nil && m.TaskBackground.Image != "" {
		imgPath := filepath.Join(parentStaging, m.TaskBackground.Image)
		if err := validateThemeImageFile(imgPath); err != nil {
			return err
		}
	}

	// Replace destination atomically: rename old aside, rename new in, remove old.
	backup := ""
	if _, err := os.Stat(dest); err == nil {
		backup = dest + ".bak-" + randomThemeSuffix()
		if err := os.Rename(dest, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(parentStaging, dest); err != nil {
		if backup != "" {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	cleanupStaging = false
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func randomThemeSuffix() string {
	// Short non-crypto suffix for backup dir names.
	return fmt.Sprintf("%d", os.Getpid())
}

func copyThemeTree(src, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return filepath.Walk(srcAbs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Only allow root-level files (theme.json + up to two scene images).
		if strings.Contains(rel, string(os.PathSeparator)) || strings.Contains(rel, "/") || strings.Contains(rel, "\\") {
			return fmt.Errorf("theme package may only contain root-level files, found %q", rel)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("theme package must not contain symlinks")
		}
		if info.IsDir() {
			return fmt.Errorf("theme package must not contain subdirectories")
		}
		target := filepath.Join(dst, rel)
		// Ensure target stays inside dst.
		if !pathIsInside(dst, target) {
			return fmt.Errorf("theme path escapes destination")
		}
		return copyFileLimited(path, target, themePackMaxImageBytes+themePackMaxManifest)
	})
}

func pathIsInside(root, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func copyFileLimited(src, dst string, maxBytes int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxBytes {
		return fmt.Errorf("file too large: %s", filepath.Base(src))
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	written, err := io.Copy(out, io.LimitReader(in, maxBytes+1))
	if err != nil {
		return err
	}
	if written > maxBytes {
		return fmt.Errorf("file too large: %s", filepath.Base(src))
	}
	return out.Sync()
}

func deleteUserTheme(id string) error {
	id = strings.TrimSpace(id)
	if !themePackIDRe.MatchString(id) {
		return fmt.Errorf("invalid theme id")
	}
	if isReservedThemeID(id) {
		return fmt.Errorf("built-in themes cannot be deleted")
	}
	dest := themeDir(id)
	if !pathIsInside(themesRootDir(), dest) {
		return fmt.Errorf("refusing to delete path outside themes root")
	}
	if _, err := os.Stat(dest); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("theme %q not found", id)
		}
		return err
	}
	return os.RemoveAll(dest)
}

// resolveActiveThemeID returns a loadable official/user theme id, or empty.
// Base style ids are never active packs under schema v2.
func resolveActiveThemeID(st ThemeDesktopState) string {
	id := strings.TrimSpace(st.ActiveThemeID)
	if id == "" || isBuiltinThemeID(id) {
		return ""
	}
	if isOfficialThemeID(id) {
		return id
	}
	if userThemeExists(id) {
		// Quick re-validate; corrupt themes fall back to none (caller falls to base style).
		if _, err := loadUserThemeManifest(id); err == nil {
			return id
		}
	}
	return ""
}

func themeFileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, themePackMaxImageBytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

type themeStagingImage struct {
	path  string
	bytes []byte
}

func writeThemeStaging(m *ThemePackManifest, imagePath string, imageBytes []byte, taskImages ...themeStagingImage) (staging string, err error) {
	if err := validateThemePackManifest(m); err != nil {
		return "", err
	}
	staging, err = os.MkdirTemp("", "reasonix-theme-stage-*")
	if err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()

	writeImage := func(imageName, imagePath string, imageBytes []byte, label string) error {
		var data []byte
		switch {
		case len(imageBytes) > 0:
			data = imageBytes
		case imagePath != "":
			data, err = os.ReadFile(imagePath)
			if err != nil {
				return fmt.Errorf("read %s image: %w", label, err)
			}
		default:
			return fmt.Errorf("%s image data is required", label)
		}
		if int64(len(data)) > themePackMaxImageBytes {
			return fmt.Errorf("%s image exceeds %d bytes", label, themePackMaxImageBytes)
		}
		imgDest := filepath.Join(staging, imageName)
		if err := os.WriteFile(imgDest, data, 0o644); err != nil {
			return err
		}
		if err := validateThemeImageFile(imgDest); err != nil {
			return err
		}
		return nil
	}

	// Place scene images first so names and image content are re-validated.
	if m.Background != nil && m.Background.Image != "" {
		if err := writeImage(m.Background.Image, imagePath, imageBytes, "home background"); err != nil {
			return "", err
		}
	}
	if m.TaskBackground != nil && m.TaskBackground.Image != "" {
		var task themeStagingImage
		if len(taskImages) > 0 {
			task = taskImages[0]
		}
		if err := writeImage(m.TaskBackground.Image, task.path, task.bytes, "task background"); err != nil {
			return "", err
		}
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if len(raw) > themePackMaxManifest {
		return "", fmt.Errorf("theme manifest exceeds %d bytes", themePackMaxManifest)
	}
	if err := os.WriteFile(filepath.Join(staging, themePackManifestName), raw, 0o644); err != nil {
		return "", err
	}
	cleanup = false
	return staging, nil
}

func resolveThemeImageAbs(id, imageName string) (string, error) {
	id = strings.TrimSpace(id)
	imageName = filepath.Base(strings.TrimSpace(imageName))
	if !themePackIDRe.MatchString(id) || isReservedThemeID(id) {
		return "", fmt.Errorf("invalid theme id")
	}
	if !themePackImageRe.MatchString(imageName) {
		return "", fmt.Errorf("invalid image name")
	}
	root := themeDir(id)
	abs := filepath.Join(root, imageName)
	if !pathIsInside(root, abs) {
		return "", fmt.Errorf("image path escapes theme directory")
	}
	// Resolve symlinks and re-check containment (TOCTOU defense on serve).
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If the file doesn't exist yet, still return the intended path after containment check.
		if os.IsNotExist(err) {
			return abs, nil
		}
		return "", err
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootResolved = root
	}
	if !pathIsInside(rootResolved, resolved) {
		return "", fmt.Errorf("image path escapes theme directory")
	}
	return resolved, nil
}
