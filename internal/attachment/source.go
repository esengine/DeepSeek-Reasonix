package attachment

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Source is one local image to admit. Exactly one of Bytes, Path, or DataURL
// must be set. Path is resolved against WorkspaceRoot when relative.
type Source struct {
	DisplayName   string
	DeclaredMIME  string
	Bytes         []byte
	Path          string
	WorkspaceRoot string
	Confine       string
	DataURL       string
	Existing      *AttachmentRef
}

func (s Source) displayName() string {
	if strings.TrimSpace(s.DisplayName) != "" {
		return NormalizeDisplayName(s.DisplayName)
	}
	if s.Path != "" {
		return NormalizeDisplayName(filepath.Base(s.Path))
	}
	return "image"
}

func (s Source) Read(ctx context.Context, policy Policy) (PreparedImage, error) {
	if err := ctx.Err(); err != nil {
		return PreparedImage{}, canceledError(err)
	}
	if s.Existing != nil {
		if err := s.Existing.Validate(); err != nil {
			return PreparedImage{}, err
		}
		return PreparedImage{DisplayName: s.displayName(), MIME: s.Existing.MIME(), Width: s.Existing.Width, Height: s.Existing.Height, Existing: s.Existing}, nil
	}
	raw, err := s.loadBytes(policy)
	if err != nil {
		err = annotateName(err, s.displayName())
		return PreparedImage{}, err
	}
	verified, err := verifyImageBytes(raw, s.DeclaredMIME, policy)
	if err != nil {
		err = annotateName(err, s.displayName())
		return PreparedImage{}, err
	}
	return PreparedImage{
		DisplayName: s.displayName(),
		MIME:        verified.MIME,
		Width:       verified.Width,
		Height:      verified.Height,
		Bytes:       verified.Bytes,
	}, nil
}

func (s Source) loadBytes(policy Policy) ([]byte, error) {
	switch {
	case len(s.Bytes) > 0 && s.Path == "" && s.DataURL == "":
		if int64(len(s.Bytes)) > policy.MaxBytes {
			return nil, Error{Code: CodeSize, Message: defaultDetail(CodeSize)}
		}
		return s.Bytes, nil
	case s.DataURL != "" && s.Path == "" && len(s.Bytes) == 0:
		return decodeDataURL(s.DataURL, policy.MaxBytes)
	case s.Path != "" && s.DataURL == "" && len(s.Bytes) == 0:
		return readPathBytes(s.WorkspaceRoot, s.Path, s.Confine, policy.MaxBytes)
	default:
		return nil, Error{Code: CodeUnsupported, Message: "image source is malformed"}
	}
}

func decodeDataURL(dataURL string, maxBytes int64) ([]byte, error) {
	const marker = ";base64,"
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, Error{Code: CodeUnsupported, Message: defaultDetail(CodeUnsupported)}
	}
	i := strings.Index(dataURL, marker)
	if i <= len("data:") {
		return nil, Error{Code: CodeUnsupported, Message: defaultDetail(CodeUnsupported)}
	}
	encoded := dataURL[i+len(marker):]
	if int64(len(encoded)) > ((maxBytes+2)/3)*4 {
		return nil, Error{Code: CodeSize, Message: defaultDetail(CodeSize)}
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, Error{Code: CodeCorrupt, Message: defaultDetail(CodeCorrupt), Cause: err}
	}
	if len(raw) == 0 || int64(len(raw)) > maxBytes {
		return nil, Error{Code: CodeSize, Message: defaultDetail(CodeSize)}
	}
	return raw, nil
}

func readPathBytes(root, path, confine string, maxBytes int64) ([]byte, error) {
	resolved, err := resolveSourcePath(root, path)
	if err != nil {
		return nil, err
	}
	if confine != "" {
		absRoot, rootErr := filepath.Abs(root)
		if rootErr != nil {
			return nil, Error{Code: CodeUnsafe, Message: defaultDetail(CodeUnsafe), Cause: rootErr}
		}
		confineAbs := filepath.Join(absRoot, filepath.FromSlash(confine))
		rel, relErr := filepath.Rel(confineAbs, resolved)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			return nil, Error{Code: CodeUnsafe, Message: defaultDetail(CodeUnsafe)}
		}
	}
	// Resolve through an open workspace root. Parent symlink replacement must
	// never turn a validated attachment path into a host filesystem read.
	var bounded *os.Root
	readName := resolved
	if confine != "" {
		bounded, err = os.OpenRoot(root)
		if err != nil {
			return nil, Error{Code: CodeUnreadable, Message: defaultDetail(CodeUnreadable), Cause: err}
		}
		defer bounded.Close()
		absRoot, _ := filepath.Abs(root)
		readName, err = filepath.Rel(absRoot, resolved)
		if err != nil || !filepath.IsLocal(readName) {
			return nil, Error{Code: CodeUnsafe, Message: defaultDetail(CodeUnsafe)}
		}
	}
	lstat := os.Lstat
	open := func(name string) (*os.File, error) { return os.OpenFile(name, imageReadFlags, 0) }
	if bounded != nil {
		lstat = bounded.Lstat
		open = func(name string) (*os.File, error) { return bounded.OpenFile(name, imageReadFlags, 0) }
	}
	info, err := lstat(readName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, Error{Code: CodeMissing, Message: defaultDetail(CodeMissing), Cause: err, Retry: true}
		}
		return nil, Error{Code: CodeUnreadable, Message: defaultDetail(CodeUnreadable), Cause: err, Retry: true}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, Error{Code: CodeUnsafe, Message: defaultDetail(CodeUnsafe)}
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBytes {
		return nil, Error{Code: CodeSize, Message: defaultDetail(CodeSize)}
	}
	f, err := open(readName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, Error{Code: CodeMissing, Message: defaultDetail(CodeMissing), Cause: err, Retry: true}
		}
		return nil, Error{Code: CodeUnreadable, Message: defaultDetail(CodeUnreadable), Cause: err, Retry: true}
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, Error{Code: CodeUnreadable, Message: defaultDetail(CodeUnreadable), Cause: err, Retry: true}
	}
	if !os.SameFile(info, opened) {
		return nil, Error{Code: CodeChanged, Message: defaultDetail(CodeChanged), Retry: true}
	}
	if !sameSourceEntry(lstat, readName, opened) {
		return nil, Error{Code: CodeChanged, Message: defaultDetail(CodeChanged), Retry: true}
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, Error{Code: CodeUnreadable, Message: defaultDetail(CodeUnreadable), Cause: err, Retry: true}
	}
	if len(raw) == 0 || int64(len(raw)) > maxBytes {
		return nil, Error{Code: CodeSize, Message: defaultDetail(CodeSize)}
	}
	if after, err := f.Stat(); err != nil {
		return nil, Error{Code: CodeUnreadable, Message: defaultDetail(CodeUnreadable), Cause: err, Retry: true}
	} else if !os.SameFile(opened, after) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return nil, Error{Code: CodeChanged, Message: defaultDetail(CodeChanged), Retry: true}
	}
	if !sameSourceEntry(lstat, readName, opened) {
		return nil, Error{Code: CodeChanged, Message: defaultDetail(CodeChanged), Retry: true}
	}
	return raw, nil
}

func sameSourceEntry(lstat func(string) (os.FileInfo, error), name string, opened os.FileInfo) bool {
	current, err := lstat(name)
	return err == nil && current.Mode()&os.ModeSymlink == 0 && os.SameFile(opened, current)
}

// ReadWorkspaceImageBytes shares confined, race-resistant file access with
// legacy preview callers, which retain their own decoding policy.
func ReadWorkspaceImageBytes(root, path string, maxBytes int64) ([]byte, error) {
	return readPathBytes(root, path, ".reasonix/attachments", maxBytes)
}

func resolveSourcePath(root, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", Error{Code: CodeUnsafe, Message: defaultDetail(CodeUnsafe)}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return "", Error{Code: CodeUnsafe, Message: "workspace root is required for relative image paths"}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", Error{Code: CodeUnsafe, Message: defaultDetail(CodeUnsafe), Cause: err}
	}
	joined := filepath.Join(absRoot, filepath.FromSlash(path))
	rel, err := filepath.Rel(absRoot, joined)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", Error{Code: CodeUnsafe, Message: defaultDetail(CodeUnsafe)}
	}
	return joined, nil
}

func annotateName(err error, name string) error {
	var item Error
	if errorsAs(err, &item) {
		if item.Name == "" {
			item.Name = name
		}
		return item
	}
	return err
}

func errorsAs(err error, target *Error) bool {
	return errors.As(err, target)
}
