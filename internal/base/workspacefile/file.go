// Package workspacefile provides bounded, root-confined document reads.
package workspacefile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	TextLimit   = 2 << 20
	ImageLimit  = 10 << 20
	PDFLimit    = 32 << 20
	OfficeLimit = 32 << 20
)

var (
	ErrDenied      = errors.New("file is outside authorized roots")
	ErrMissing     = errors.New("file does not exist")
	ErrNotFile     = errors.New("not a regular file")
	ErrTooLarge    = errors.New("file exceeds preview limit")
	ErrNotText     = errors.New("file is not UTF-8 text")
	ErrUnsupported = errors.New("file format has no preview")
	ErrChanged     = errors.New("file changed during preview read")
)

type Ref struct {
	Path string `json:"path"`
	Root string `json:"root,omitempty"`
}

type Info struct {
	Ref      Ref    `json:"ref"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	Version  string `json:"version"`
	Limit    int64  `json:"limit"`
	ReadOnly bool   `json:"readOnly"`
}

// Service receives roots already authorized by its host.
type Service struct {
	Primary string
	Extra   []string
}

func (s Service) Resolve(path string) (Ref, error) {
	if !filepath.IsAbs(path) {
		if !filepath.IsLocal(path) {
			return Ref{}, ErrDenied
		}
		return Ref{Path: filepath.ToSlash(filepath.Clean(path))}, nil
	}
	for _, root := range append([]string{s.Primary}, s.Extra...) {
		rel, err := filepath.Rel(root, path)
		if err == nil && filepath.IsLocal(rel) {
			ref := Ref{Path: filepath.ToSlash(rel)}
			if filepath.Clean(root) != filepath.Clean(s.Primary) {
				ref.Root = root
			}
			return ref, nil
		}
	}
	return Ref{}, ErrDenied
}

func (s Service) open(ref Ref) (*os.File, error) {
	path := filepath.FromSlash(ref.Path)
	if !filepath.IsLocal(path) || path == "." {
		return nil, ErrDenied
	}
	base := s.Primary
	if ref.Root != "" && filepath.Clean(ref.Root) != filepath.Clean(base) {
		found := false
		for _, extra := range s.Extra {
			if filepath.Clean(extra) == filepath.Clean(ref.Root) {
				base, found = extra, true
				break
			}
		}
		if !found {
			return nil, ErrDenied
		}
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, classify(err)
	}
	defer root.Close()
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil, classify(err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(base, path))
	if err != nil {
		return nil, classify(err)
	}
	relative, err := filepath.Rel(resolvedBase, target)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, ErrDenied
	}
	// Stat before opening excludes FIFOs and devices, which may block on open.
	info, err := root.Stat(path)
	if err != nil {
		return nil, classify(err)
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotFile
	}
	file, err := root.OpenFile(path, os.O_RDONLY|nonblock, 0)
	if err != nil {
		return nil, classify(err)
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err != nil {
			return nil, classify(err)
		}
		return nil, ErrNotFile
	}
	return file, nil
}

func classify(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrMissing, err)
	}
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w: %w", ErrDenied, err)
	}
	return err
}

func metadata(ref Ref, info fs.FileInfo) Info {
	kind, limit := Format(ref.Path)
	return Info{Ref: ref, Kind: kind, Size: info.Size(), Limit: limit, ReadOnly: ref.Root != "",
		Version: strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10)}
}

func (s Service) Info(ctx context.Context, ref Ref) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	f, err := s.open(ref)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	return metadata(ref, info), nil
}

func (s Service) Content(ctx context.Context, ref Ref) ([]byte, Info, error) {
	return s.content(ctx, ref, false)
}

func (s Service) Text(ctx context.Context, ref Ref) ([]byte, Info, error) {
	return s.content(ctx, ref, true)
}

func (s Service) content(ctx context.Context, ref Ref, textOnly bool) ([]byte, Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, Info{}, err
	}
	f, err := s.open(ref)
	if err != nil {
		return nil, Info{}, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return nil, Info{}, err
	}
	info := metadata(ref, before)
	if textOnly {
		info.Kind, info.Limit = "text", TextLimit
	}
	if info.Kind == "unsupported" {
		return nil, info, ErrUnsupported
	}
	if info.Size > info.Limit {
		return nil, info, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, info.Limit+1))
	if err != nil {
		return nil, info, err
	}
	if err := ctx.Err(); err != nil {
		return nil, info, err
	}
	if int64(len(data)) > info.Limit {
		return nil, info, ErrTooLarge
	}
	after, err := f.Stat()
	if err != nil {
		return nil, info, err
	}
	if metadata(ref, after).Version != info.Version {
		return nil, info, ErrChanged
	}
	if info.Kind != "pdf" && info.Kind != "image" && info.Kind != "office" && (!utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0) {
		return nil, info, ErrNotText
	}
	return data, info, nil
}

func Format(path string) (string, int64) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return "markdown", TextLimit
	case ".html", ".htm":
		return "html", TextLimit
	case ".csv", ".tsv":
		return "table", TextLimit
	case ".pdf":
		return "pdf", PDFLimit
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".bmp", ".ico", ".svg":
		return "image", ImageLimit
	case ".docx", ".xlsx":
		return "office", OfficeLimit
	case ".doc", ".xls", ".ppt", ".pptx", ".zip", ".gz", ".7z", ".rar", ".exe", ".dll", ".bin":
		return "unsupported", 0
	default:
		return "text", TextLimit
	}
}
