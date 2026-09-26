package control

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/model/visionimage"
)

const maxImageAttachmentBytes = 10 * 1024 * 1024
const maxFileAttachmentBytes = 25 * 1024 * 1024
const maxAttachmentCreateAttempts = 1000

var attachmentPathSeq atomic.Uint64
var attachmentNow = time.Now
var safeAttachmentExt = regexp.MustCompile(`^\.[a-z0-9]{1,12}$`)

func SaveAttachmentBytesInRoot(root, origName string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxFileAttachmentBytes {
		return "", fmt.Errorf("attachment must be between 1 byte and 25 MB")
	}
	ext := strings.ToLower(filepath.Ext(origName))
	if !safeAttachmentExt.MatchString(ext) {
		ext = ".bin"
	}
	return saveAttachmentBytesInRoot(root, ext, raw)
}

func SaveImageBytes(declaredMime string, raw []byte) (string, error) {
	return SaveImageBytesInRoot(".", declaredMime, raw)
}

func SaveImageBytesInRoot(root, declaredMime string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return "", fmt.Errorf("pasted image must be between 1 byte and 10 MB")
	}
	mime := visionimage.DetectMime(raw)
	if mime == "" {
		return "", fmt.Errorf("pasted data is not a supported image")
	}
	if declaredMime != "" && visionimage.Ext(declaredMime) == "" {
		return "", fmt.Errorf("unsupported image type: %s", declaredMime)
	}
	ext := visionimage.Ext(mime)
	return saveAttachmentBytesInRoot(root, ext, raw)
}

func saveAttachmentBytesInRoot(root, ext string, raw []byte) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := ensureAttachmentRootIn(absRoot); err != nil {
		return "", err
	}
	rel, f, err := createAttachmentFileIn(absRoot, ext)
	if err != nil {
		return "", err
	}
	if n, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(absRoot, rel))
		return "", err
	} else if n != len(raw) {
		_ = f.Close()
		_ = os.Remove(filepath.Join(absRoot, rel))
		return "", io.ErrShortWrite
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filepath.Join(absRoot, rel))
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// visionImageDataURL reads an attachment and, unlike ImageDataURL (which feeds
// the desktop preview at full resolution), downscales/recompresses it before
// base64 so an oversized photo doesn't balloon the request bytes and image
// tokens. An attachment that cannot be fitted is refused, never forwarded.
func visionImageDataURL(base, path string) (string, error) {
	raw, mime, err := readAttachmentImage(base, path)
	if err != nil {
		return "", err
	}
	raw, mime, err = visionimage.Fit(raw, mime)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

func readAttachmentImage(base, path string) (raw []byte, mime string, err error) {
	clean, err := cleanAttachmentPath(base, path)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("attachment path must not be a symlink")
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxImageAttachmentBytes {
		return nil, "", fmt.Errorf("attachment image must be between 1 byte and 10 MB")
	}
	f, err := os.Open(clean)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if !os.SameFile(info, opened) {
		return nil, "", fmt.Errorf("attachment changed while opening")
	}
	raw, err = io.ReadAll(io.LimitReader(f, maxImageAttachmentBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return nil, "", fmt.Errorf("attachment image must be between 1 byte and 10 MB")
	}
	if after, err := f.Stat(); err != nil {
		return nil, "", err
	} else if !os.SameFile(opened, after) || after.Size() != opened.Size() {
		return nil, "", fmt.Errorf("attachment changed while reading")
	}
	mime = visionimage.DetectMime(raw)
	if mime == "" {
		return nil, "", fmt.Errorf("attachment is not an image")
	}
	return raw, mime, nil
}

// cleanAttachmentPath resolves a stored reference under base. The reference
// itself stays relative and inside .reasonix/attachments; base only says which
// workspace that is. Without it the path resolves against the process working
// directory — which is the workspace for the CLI and "/" for a window launched
// from Finder, so every attachment a desktop session pasted read as missing.
func cleanAttachmentPath(base, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("attachment path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	root := filepath.Join(".reasonix", "attachments")
	if clean == "." || clean == root || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || !strings.HasPrefix(clean, root+string(filepath.Separator)) {
		return "", fmt.Errorf("attachment path is outside .reasonix/attachments")
	}
	if strings.TrimSpace(base) == "" {
		base = "."
	}
	if err := ensureAttachmentRootIn(base); err != nil {
		return "", err
	}
	if err := rejectSymlinkComponents(filepath.Join(base, clean), filepath.Join(base, root)); err != nil {
		return "", err
	}
	return filepath.Join(base, clean), nil
}

func rejectSymlinkComponents(path, root string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return fmt.Errorf("attachment path is outside .reasonix/attachments")
	}
	cur := root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment path must not contain symlinks")
		}
	}
	return nil
}

func ensureAttachmentRoot() error {
	return ensureAttachmentRootIn(".")
}

func ensureAttachmentRootIn(base string) error {
	root := filepath.Join(base, ".reasonix", "attachments")
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment directory must not be a symlink")
		}
		if !info.IsDir() {
			return fmt.Errorf("attachment path exists but is not a directory")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("attachment directory is invalid")
	}
	return nil
}

func createAttachmentFile(ext string) (string, *os.File, error) {
	return createAttachmentFileIn(".", ext)
}

func createAttachmentFileIn(base, ext string) (string, *os.File, error) {
	_ = fileutil.MarkUntracked(filepath.Join(base, ".reasonix", "attachments"))
	for range maxAttachmentCreateAttempts {
		rel := attachmentPath(ext)
		f, err := os.OpenFile(filepath.Join(base, rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return rel, f, nil
	}
	return "", nil, fmt.Errorf("create unique attachment path")
}

func attachmentPath(ext string) string {
	seq := attachmentPathSeq.Add(1)
	name := fmt.Sprintf("clipboard-%s-%06d%s", attachmentNow().Format("20060102-150405.000000"), seq, ext)
	return filepath.Join(".reasonix", "attachments", name)
}
