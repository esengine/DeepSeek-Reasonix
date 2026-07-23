// Package sftpfs is the SFTP file layer for the remote module: directory
// listing, stat, capped reads with text/binary detection, atomic writes, and
// the usual mkdir/rename/remove. It quarantines the github.com/pkg/sftp
// dependency — no other Reasonix package imports it directly. One *FS is shared
// per SSH connection; the underlying pkg/sftp client is safe for concurrent
// use.
package sftpfs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// FS wraps an SFTP client bound to one SSH connection.
type FS struct {
	client *sftp.Client
}

// Entry is one directory entry.
type Entry struct {
	Name    string
	Path    string
	Size    int64
	Mode    fs.FileMode
	ModTime int64 // unix seconds
	IsDir   bool
	Symlink bool
}

// New opens an SFTP session over an established SSH client.
func New(cl *ssh.Client) (*FS, error) {
	c, err := sftp.NewClient(cl)
	if err != nil {
		return nil, err
	}
	return &FS{client: c}, nil
}

// Close tears down the SFTP session (not the SSH connection).
func (f *FS) Close() error {
	if f == nil || f.client == nil {
		return nil
	}
	return f.client.Close()
}

// run executes op in a goroutine and honors ctx cancellation. pkg/sftp has no
// context-aware API; on cancellation we abandon (not abort) the in-flight op —
// it completes in the background and its result is discarded.
func run[T any](ctx context.Context, op func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := op()
		ch <- result{v, err}
	}()
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case r := <-ch:
		return r.val, r.err
	}
}

// List returns the entries of dir.
func (f *FS) List(ctx context.Context, dir string) ([]Entry, error) {
	return run(ctx, func() ([]Entry, error) {
		infos, err := f.client.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		out := make([]Entry, 0, len(infos))
		for _, fi := range infos {
			full := path.Join(dir, fi.Name())
			e := Entry{
				Name:    fi.Name(),
				Path:    full,
				Size:    fi.Size(),
				Mode:    fi.Mode(),
				ModTime: fi.ModTime().Unix(),
				IsDir:   fi.IsDir(),
				Symlink: fi.Mode()&fs.ModeSymlink != 0,
			}
			// Resolve symlink dir-ness so the tree can show expanders.
			if e.Symlink {
				if st, serr := f.client.Stat(full); serr == nil {
					e.IsDir = st.IsDir()
					e.Size = st.Size()
				}
			}
			out = append(out, e)
		}
		return out, nil
	})
}

// Stat returns metadata for a single path (following symlinks).
func (f *FS) Stat(ctx context.Context, p string) (Entry, error) {
	return run(ctx, func() (Entry, error) {
		fi, err := f.client.Stat(p)
		if err != nil {
			return Entry{}, err
		}
		return Entry{
			Name:    path.Base(p),
			Path:    p,
			Size:    fi.Size(),
			Mode:    fi.Mode(),
			ModTime: fi.ModTime().Unix(),
			IsDir:   fi.IsDir(),
		}, nil
	})
}

// ReadFile reads up to maxSize bytes (0 => DefaultReadCap). It reports
// truncated=true when the file exceeds the cap, and returns the detected Kind.
func (f *FS) ReadFile(ctx context.Context, p string, maxSize int64) (data []byte, truncated bool, kind Kind, err error) {
	if maxSize <= 0 {
		maxSize = DefaultReadCap
	}
	type res struct {
		data      []byte
		truncated bool
		kind      Kind
	}
	r, err := run(ctx, func() (res, error) {
		fh, oerr := f.client.Open(p)
		if oerr != nil {
			return res{}, oerr
		}
		defer fh.Close()
		// Read one extra byte to detect truncation.
		buf, rerr := io.ReadAll(io.LimitReader(fh, maxSize+1))
		if rerr != nil {
			return res{}, rerr
		}
		out := res{}
		if int64(len(buf)) > maxSize {
			out.truncated = true
			buf = buf[:maxSize]
		}
		out.data = buf
		out.kind = DetectKind(buf)
		return out, nil
	})
	if err != nil {
		return nil, false, KindBinary, err
	}
	return r.data, r.truncated, r.kind, nil
}

// Download streams the entire remote file p to w with no size cap. Use this for
// `fs get`-style whole-file transfers; ReadFile is the capped preview path and
// must not be used to download files (it silently truncates at DefaultReadCap).
// Returns the number of bytes copied.
func (f *FS) Download(ctx context.Context, p string, w io.Writer) (int64, error) {
	return run(ctx, func() (int64, error) {
		fh, oerr := f.client.Open(p)
		if oerr != nil {
			return 0, oerr
		}
		defer fh.Close()
		return io.Copy(w, fh)
	})
}

// WriteFileAtomic writes data to p via a temp file in the same directory
// followed by a rename, so a concurrent reader never sees a partial file.
func (f *FS) WriteFileAtomic(ctx context.Context, p string, data []byte, perm fs.FileMode) error {
	_, err := f.writeFileAtomic(ctx, p, bytes.NewReader(data), perm)
	return err
}

// UploadAtomic streams r into a same-directory temporary file and publishes it
// with the same atomic-write contract as WriteFileAtomic.
func (f *FS) UploadAtomic(ctx context.Context, p string, r io.Reader, perm fs.FileMode) (int64, error) {
	return f.writeFileAtomic(ctx, p, r, perm)
}

func (f *FS) writeFileAtomic(ctx context.Context, p string, r io.Reader, perm fs.FileMode) (int64, error) {
	return run(ctx, func() (int64, error) {
		dir := path.Dir(p)
		tmp := path.Join(dir, "."+path.Base(p)+".reasonix-tmp-"+randSuffix())
		fh, oerr := f.client.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if oerr != nil {
			return 0, oerr
		}
		n, werr := io.Copy(fh, r)
		if werr != nil {
			_ = fh.Close()
			_ = f.client.Remove(tmp)
			return n, werr
		}
		if cerr := fh.Close(); cerr != nil {
			_ = f.client.Remove(tmp)
			return n, cerr
		}
		if perm != 0 {
			if cerr := f.client.Chmod(tmp, perm); cerr != nil {
				_ = f.client.Remove(tmp)
				return n, cerr
			}
		}
		if rerr := f.rename(tmp, p); rerr != nil {
			_ = f.client.Remove(tmp)
			return n, rerr
		}
		return n, nil
	})
}

// rename prefers the POSIX atomic rename extension, falling back to
// remove-then-rename when the destination exists on a server without it.
func (f *FS) rename(oldPath, newPath string) error {
	if err := f.client.PosixRename(oldPath, newPath); err == nil {
		return nil
	}
	if err := f.client.Rename(oldPath, newPath); err == nil {
		return nil
	}
	// Destination may already exist on a plain-SFTP server: remove and retry.
	if _, serr := f.client.Stat(newPath); serr == nil {
		if rerr := f.client.Remove(newPath); rerr != nil {
			return rerr
		}
	}
	return f.client.Rename(oldPath, newPath)
}

// MkdirAll creates p and any missing parents.
func (f *FS) MkdirAll(ctx context.Context, p string) error {
	_, err := run(ctx, func() (struct{}, error) {
		return struct{}{}, f.client.MkdirAll(p)
	})
	return err
}

// MkdirExclusive creates exactly p and fails when it already exists. It is the
// atomic primitive used by cross-client remote bootstrap locks.
func (f *FS) MkdirExclusive(ctx context.Context, p string) error {
	_, err := run(ctx, func() (struct{}, error) {
		return struct{}{}, f.client.Mkdir(p)
	})
	return err
}

// Rename moves oldPath to newPath.
func (f *FS) Rename(ctx context.Context, oldPath, newPath string) error {
	_, err := run(ctx, func() (struct{}, error) {
		return struct{}{}, f.rename(oldPath, newPath)
	})
	return err
}

// Remove deletes a file or (recursively) a directory.
func (f *FS) Remove(ctx context.Context, p string, recursive bool) error {
	_, err := run(ctx, func() (struct{}, error) {
		fi, serr := f.client.Stat(p)
		if serr != nil {
			return struct{}{}, serr
		}
		if fi.IsDir() {
			if recursive {
				return struct{}{}, f.client.RemoveAll(p)
			}
			return struct{}{}, f.client.RemoveDirectory(p)
		}
		return struct{}{}, f.client.Remove(p)
	})
	return err
}

// RealPath resolves ~, relative, and symlinked paths to an absolute path on
// the remote host.
func (f *FS) RealPath(ctx context.Context, p string) (string, error) {
	return run(ctx, func() (string, error) {
		if p == "~" || strings.HasPrefix(p, "~/") {
			home, herr := f.client.Getwd() // sftp opens at the login home
			if herr == nil {
				p = path.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
			}
		}
		rp, err := f.client.RealPath(p)
		if err != nil {
			return "", err
		}
		return rp, nil
	})
}

func randSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
