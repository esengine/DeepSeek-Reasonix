package delta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// ErrFileMismatch is a staged file that is not the file the Index describes.
var ErrFileMismatch = errors.New("delta: staged file does not match the index")

// Fetcher returns a stored (compressed) chunk by hash.
type Fetcher func(ctx context.Context, hash string) ([]byte, error)

// Fetch downloads every missing chunk into cacheDir as its plain bytes, each
// checked against its name before it is kept. A chunk already cached is not
// fetched again, so an interrupted update resumes where it stopped.
func Fetch(ctx context.Context, missing []Chunk, fetch Fetcher, cacheDir string, parallel int, progress func(done, total int64)) error {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}
	var total int64
	for _, c := range missing {
		total += int64(c.Size)
	}
	var done atomic.Int64
	jobs := make(chan Chunk)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	for range max(parallel, 1) {
		wg.Go(func() {
			for c := range jobs {
				if err := fetchOne(ctx, c, fetch, cacheDir); err != nil {
					select {
					case errs <- err:
					default:
					}
					continue
				}
				if progress != nil {
					progress(done.Add(int64(c.Size)), total)
				}
			}
		})
	}
	for _, c := range missing {
		select {
		case jobs <- c:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errs:
		return err
	default:
		return ctx.Err()
	}
}

func fetchOne(ctx context.Context, c Chunk, fetch Fetcher, cacheDir string) error {
	dst := filepath.Join(cacheDir, c.Hash)
	if b, err := os.ReadFile(dst); err == nil && HashOf(b) == c.Hash {
		return nil
	}
	stored, err := fetch(ctx, c.Hash)
	if err != nil {
		return fmt.Errorf("fetch chunk %s: %w", c.Hash, err)
	}
	plain, err := Decompress(stored, c.Hash)
	if err != nil {
		return err
	}
	tmp := dst + ".part"
	if err := os.WriteFile(tmp, plain, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// Assemble writes x's tree under stagingDir from the install's own chunks and
// the fetched ones, and checks every file against x. It returns nil only when
// the staged tree is exactly the release.
func Assemble(x Index, p Plan, installRoot, cacheDir, stagingDir string) error {
	open := map[string]*os.File{}
	defer func() {
		for _, f := range open {
			f.Close()
		}
	}()
	local := func(src Source, size int) ([]byte, error) {
		f := open[src.Path]
		if f == nil {
			var err error
			if f, err = os.Open(filepath.Join(installRoot, filepath.FromSlash(src.Path))); err != nil {
				return nil, err
			}
			open[src.Path] = f
		}
		b := make([]byte, size)
		_, err := f.ReadAt(b, src.Off)
		return b, err
	}
	for _, f := range x.Files {
		if err := assembleFile(f, p, local, cacheDir, stagingDir); err != nil {
			return err
		}
	}
	return nil
}

func assembleFile(f File, p Plan, local func(Source, int) ([]byte, error), cacheDir, stagingDir string) error {
	dst := filepath.Join(stagingDir, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if f.Exec {
		mode = 0o755
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	h := sha256.New()
	w := io.MultiWriter(out, h)
	for _, c := range f.Chunks {
		var b []byte
		if src, ok := p.Local[c.Hash]; ok {
			b, err = local(src, c.Size)
		} else {
			b, err = os.ReadFile(filepath.Join(cacheDir, c.Hash))
		}
		if err == nil && HashOf(b) != c.Hash {
			err = fmt.Errorf("%w: chunk %s of %s", ErrChunkMismatch, c.Hash, f.Path)
		}
		if err == nil {
			_, err = w.Write(b)
		}
		if err != nil {
			out.Close()
			return fmt.Errorf("assemble %s: %w", f.Path, err)
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != f.SHA256 {
		return fmt.Errorf("%w: %s", ErrFileMismatch, f.Path)
	}
	return nil
}
