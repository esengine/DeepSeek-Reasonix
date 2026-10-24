package fileutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

type ContentVersion struct {
	Kind          string `json:"kind"`
	Digest        string `json:"digest,omitempty"`
	Mode          uint32 `json:"mode,omitempty"`
	BufferVersion string `json:"bufferVersion,omitempty"`
}

var ErrContentChanged = errors.New("file changed while reading content version")

func ReadContentVersion(ctx context.Context, path string) (ContentVersion, error) {
	if err := ctx.Err(); err != nil {
		return ContentVersion{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ContentVersion{Kind: "absent"}, nil
	}
	if err != nil {
		return ContentVersion{}, err
	}
	v := ContentVersion{Mode: uint32(info.Mode().Perm())}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return ContentVersion{}, err
		}
		sum := sha256.Sum256([]byte(target))
		v.Kind, v.Digest = "symlink", hex.EncodeToString(sum[:])
	case info.Mode().IsRegular():
		f, err := os.Open(path)
		if err != nil {
			return ContentVersion{}, err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, &versionReader{ctx: ctx, r: f}); err != nil {
			return ContentVersion{}, err
		}
		after, err := f.Stat()
		if err != nil {
			return ContentVersion{}, err
		}
		if !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
			return ContentVersion{}, ErrContentChanged
		}
		v.Kind, v.Digest = "file", hex.EncodeToString(h.Sum(nil))
	case info.IsDir():
		v.Kind = "directory"
	default:
		return ContentVersion{}, errors.ErrUnsupported
	}
	return v, nil
}

type versionReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *versionReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
