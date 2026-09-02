// Package fileref converts workspace-local image files into provider-visible
// base64 data URLs. It is deliberately transport-agnostic: control resolves
// @-references into candidates, and agent's task dispatch resolves tool-call
// image parameters through the same validated pipeline (issue #6530).
package fileref

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// MaxImageAttachmentBytes is the byte ceiling for one image file, matching the
// attachment pipeline so every image entering a provider request shares one
// bound.
const MaxImageAttachmentBytes = 64 * 1024 * 1024

// FileImageDataURL resolves path against baseDir (workspace-relative or
// absolute, confined under baseDir) and returns a base64 data URL. It applies
// the full attachment security matrix: os.OpenRoot confinement, symlink
// rejection, the 1 B–MaxImageAttachmentBytes size window, a TOCTOU same-file
// check between stat and open, MIME sniffing, and vision-aware downscaling.
func FileImageDataURL(path, baseDir string) (string, error) {
	absPath, absBase, ok := resolveAbsRef(path, baseDir)
	if !ok {
		return "", os.ErrNotExist
	}
	if absBase == "" {
		return "", fmt.Errorf("workspace root is required for file image references")
	}

	root, err := os.OpenRoot(absBase)
	if err != nil {
		return "", err
	}
	defer root.Close()

	rel, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return "", err
	}

	info, err := root.Lstat(rel)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("image path must not be a symlink")
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > MaxImageAttachmentBytes {
		return "", fmt.Errorf("image must be between 1 byte and 64 MB")
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("image changed while opening")
	}
	return dataURLFromReader(f, path)
}

func dataURLFromReader(r io.Reader, path string) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxImageAttachmentBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > MaxImageAttachmentBytes {
		return "", fmt.Errorf("image must be between 1 byte and 64 MB")
	}
	mime := detectedImageMime(raw)
	if mime == "" {
		return "", fmt.Errorf("%s is not a supported image", path)
	}
	raw, mime = CompressForVision(raw, mime)
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// resolveAbsRef resolves a user-supplied path against baseDir and returns the
// absolute path plus absolute base root to sandbox I/O under. With a baseDir,
// the path is confined under it (a relative path escaping via ".." is
// rejected). With an empty baseDir, ok=false callers fall back per their own
// policy; FileImageDataURL rejects an empty base outright.
func resolveAbsRef(path, baseDir string) (absPath, absBase string, ok bool) {
	if baseDir == "" {
		return path, "", true
	}
	absBase = baseDir
	if !filepath.IsAbs(absBase) {
		var err error
		absBase, err = filepath.Abs(baseDir)
		if err != nil {
			return "", "", false
		}
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(absBase, cleaned)
	}
	rel, err := filepath.Rel(absBase, cleaned)
	if err != nil || !filepath.IsLocal(rel) {
		return "", "", false
	}
	return cleaned, absBase, true
}

func detectedImageMime(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	mime := http.DetectContentType(raw[:min(len(raw), 512)])
	if imageMimeExt(mime) == "" {
		return ""
	}
	return mime
}

func imageMimeExt(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}
