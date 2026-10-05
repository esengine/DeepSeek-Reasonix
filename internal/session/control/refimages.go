// refimages.go — turning an @-reference into pixels a model request may carry.
package control

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"reasonix/internal/model/visionimage"
)

// inputImages resolves image @-references in the turn input to data URLs so the
// turn can carry them to a vision-capable model. Best-effort: an unreadable image
// is skipped — the @ref still lands as text via ResolveRefs.
func (c *Controller) inputImages(line string) []string {
	if !c.imageInputEnabled() {
		return nil
	}
	urls, _ := c.resolveInputImageCandidates(line)
	return urls
}

// resolveInputImageCandidates resolves authorized image references without
// consulting the active model capability. The parent controller uses this only
// to hand candidates to a child; the child decides whether to embed them. A
// reference that is an image but cannot be sent is returned as skipped, so the
// turn can say so; anything that was never an image stays silent.
func (c *Controller) resolveInputImageCandidates(line string) ([]string, []error) {
	var urls []string
	var skipped []error
	for _, r := range c.detectRefs(line) {
		baseDir := c.workspaceRoot
		if r.baseDir != "" {
			baseDir = r.baseDir
		}
		url, err := visionRefImageDataURL(r, baseDir)
		switch {
		case err == nil:
			urls = append(urls, url)
		case errors.Is(err, visionimage.ErrUnfit):
			skipped = append(skipped, err)
		}
	}
	return urls, skipped
}

func visionRefImageDataURL(r ref, baseDir string) (string, error) {
	switch r.kind {
	case refImage:
		return visionImageDataURL(baseDir, r.path)
	case refFile:
		return visionFileImageDataURL(r.path, baseDir)
	default:
		return "", fmt.Errorf("reference is not an image")
	}
}

func visionFileImageDataURL(path, baseDir string) (string, error) {
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
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxImageAttachmentBytes {
		return "", fmt.Errorf("image must be between 1 byte and 10 MB")
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
	return dataURLFromImageReader(f, path)
}

func dataURLFromImageReader(r io.Reader, path string) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxImageAttachmentBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return "", fmt.Errorf("image must be between 1 byte and 10 MB")
	}
	mime := visionimage.DetectMime(raw)
	if mime == "" {
		return "", fmt.Errorf("%s is not a supported image", path)
	}
	raw, mime, err = visionimage.Fit(raw, mime)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}
