package control

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/model/visionimage"
)

// MaxImageAttachmentBytes caps a picture attachment; the desktop checks the same number before reading one.
const MaxImageAttachmentBytes = 10 * 1024 * 1024
const maxFileAttachmentBytes = 25 * 1024 * 1024
const maxAttachmentCreateAttempts = 1000

var (
	ErrAttachmentEmpty    = errors.New("attachment is empty")
	ErrAttachmentTooLarge = errors.New("attachment is too large")
	ErrAttachmentNotImage = errors.New("attachment is not a supported image")
	ErrAttachmentWrite    = errors.New("attachment could not be saved")
)

// AttachmentError is a refused attachment with the class that refused it.
// Kind is one of the Err sentinels; the other fields are the facts a reader
// needs in order to act on that class.
type AttachmentError struct {
	Kind error
	Msg  string
	// LimitMB is the cap that was exceeded, for ErrAttachmentTooLarge.
	LimitMB int
	// Type is what the bytes (or the declared media type) were taken for, for
	// ErrAttachmentNotImage.
	Type  string
	Cause error
}

func (e *AttachmentError) Error() string { return e.Msg }

func (e *AttachmentError) Unwrap() []error {
	if e.Cause != nil {
		return []error{e.Kind, e.Cause}
	}
	return []error{e.Kind}
}

func attachmentSizeError(n, limit int) error {
	if n == 0 {
		return &AttachmentError{Kind: ErrAttachmentEmpty, Msg: "attachment is empty"}
	}
	mb := limit >> 20
	return &AttachmentError{Kind: ErrAttachmentTooLarge, Msg: fmt.Sprintf("attachment is larger than %d MB", mb), LimitMB: mb}
}

func attachmentWriteError(err error) error {
	return &AttachmentError{Kind: ErrAttachmentWrite, Msg: err.Error(), Cause: err}
}

var attachmentPathSeq atomic.Uint64
var attachmentNow = time.Now
var safeAttachmentExt = regexp.MustCompile(`^\.[a-z0-9]{1,12}$`)

func SaveAttachmentBytesInRoot(root, origName string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxFileAttachmentBytes {
		return "", attachmentSizeError(len(raw), maxFileAttachmentBytes)
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
	if len(raw) == 0 || len(raw) > MaxImageAttachmentBytes {
		return "", attachmentSizeError(len(raw), MaxImageAttachmentBytes)
	}
	mime := visionimage.DetectMime(raw)
	if mime == "" {
		return "", &AttachmentError{Kind: ErrAttachmentNotImage, Msg: "pasted data is not a supported image", Type: visionimage.Sniff(raw)}
	}
	if declaredMime != "" && visionimage.Ext(declaredMime) == "" {
		return "", &AttachmentError{Kind: ErrAttachmentNotImage, Msg: fmt.Sprintf("unsupported image type: %s", declaredMime), Type: strings.ToLower(strings.TrimSpace(declaredMime))}
	}
	ext := visionimage.Ext(mime)
	return saveAttachmentBytesInRoot(root, ext, raw)
}

func saveAttachmentBytesInRoot(root, ext string, raw []byte) (string, error) {
	rel, err := storeAttachmentBytes(root, ext, raw)
	if err != nil {
		return "", attachmentWriteError(err)
	}
	return rel, nil
}

func storeAttachmentBytes(root, ext string, raw []byte) (string, error) {
	owned, err := createOwnedAttachmentFile(root, ext)
	if err != nil {
		return "", err
	}
	defer owned.cleanup()
	if n, err := owned.Write(raw); err != nil {
		return "", err
	} else if n != len(raw) {
		return "", io.ErrShortWrite
	}
	if err := owned.finish(); err != nil {
		return "", err
	}
	return filepath.ToSlash(owned.rel), nil
}

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
	if info.IsDir() || info.Size() <= 0 || info.Size() > MaxImageAttachmentBytes {
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
	raw, err = io.ReadAll(io.LimitReader(f, MaxImageAttachmentBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) == 0 || len(raw) > MaxImageAttachmentBytes {
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
	directory, err := openAttachmentRootIn(base)
	if err != nil {
		return err
	}
	return directory.Close()
}

func createAttachmentFile(ext string) (string, *os.File, error) {
	return createAttachmentFileIn(".", ext)
}

func createAttachmentFileIn(base, ext string) (string, *os.File, error) {
	owned, err := createOwnedAttachmentFile(base, ext)
	if err != nil {
		return "", nil, err
	}
	_ = owned.directory.Close()
	return owned.rel, owned.File, nil
}

func attachmentPath(ext string) string {
	seq := attachmentPathSeq.Add(1)
	name := fmt.Sprintf("clipboard-%s-%06d%s", attachmentNow().Format("20060102-150405.000000"), seq, ext)
	return filepath.Join(".reasonix", "attachments", name)
}
