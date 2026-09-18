// Package sessioncontent stores immutable session payloads by content digest.
// It deliberately owns bytes only: session ordering, authorization and model
// projection remain responsibilities of their respective services.
package sessioncontent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"reasonix/internal/filelock"
)

const (
	copyBufferBytes = 1 << 20
	// IntegrityBlockBytes bounds range verification work independently of the
	// total object size.
	IntegrityBlockBytes = 1 << 20
	// MaxReadRange bounds one allocation, not the size of an object or session.
	MaxReadRange = 8 << 20
)

// Metadata describes how callers display or interpret content. None of these
// fields participate in the storage path; identical bytes are deduplicated.
type Metadata struct {
	MediaType string
	Name      string
}

// Ref is the durable, path-independent identity of one immutable object.
type Ref struct {
	Digest         string `json:"digest"`
	Bytes          int64  `json:"bytes"`
	MediaType      string `json:"mediaType,omitempty"`
	Name           string `json:"name,omitempty"`
	IndexDigest    string `json:"indexDigest,omitempty"`
	IntegrityBlock int64  `json:"integrityBlockBytes,omitempty"`
}

type integrityIndex struct {
	Version    int      `json:"version"`
	Object     string   `json:"object"`
	Bytes      int64    `json:"bytes"`
	BlockBytes int64    `json:"blockBytes"`
	Blocks     []string `json:"blocks"`
}

// Store is a process-independent content-addressed object store. Publishing is
// no-overwrite: concurrent writers of the same digest converge on one object.
type Store struct {
	root string
}

// ObjectWitness is a process-local observation of an immutable object. It is
// suitable only for deciding whether a previously verified cache entry may be
// reused; authorization remains the caller's responsibility.
type ObjectWitness struct {
	info        os.FileInfo
	bytes       int64
	modTimeNano int64
	indexDigest string
}

func (w ObjectWitness) Same(other ObjectWitness) bool {
	return w.info != nil && other.info != nil && w.bytes == other.bytes &&
		w.modTimeNano == other.modTimeNano && w.indexDigest == other.indexDigest &&
		os.SameFile(w.info, other.info)
}

func (w ObjectWitness) Valid() bool { return w.info != nil }

func New(root string) *Store { return &Store{root: root} }

func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Put streams r into a private temporary file, fsyncs it, then atomically
// publishes the file under its SHA-256 digest. A returned Ref always names a
// complete object. Cancellation never publishes the partial temporary file.
func (s *Store) Put(ctx context.Context, r io.Reader, meta Metadata) (Ref, error) {
	if s == nil || s.root == "" {
		return Ref{}, errors.New("session content store unavailable")
	}
	if r == nil {
		return Ref{}, errors.New("session content reader is nil")
	}
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	tmpDir := filepath.Join(s.root, ".tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return Ref{}, fmt.Errorf("create session content temp directory: %w", err)
	}
	tmp, err := os.CreateTemp(tmpDir, "content-*.tmp")
	if err != nil {
		return Ref{}, fmt.Errorf("create session content temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()

	digests := newBlockDigestWriter(tmp)
	n, err := copyWithContext(ctx, digests, r)
	if err != nil {
		return Ref{}, fmt.Errorf("stage session content: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return Ref{}, fmt.Errorf("fsync session content: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return Ref{}, fmt.Errorf("protect session content: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Ref{}, fmt.Errorf("close session content: %w", err)
	}
	closed = true

	ref := Ref{
		Digest:         hex.EncodeToString(digests.full.Sum(nil)),
		Bytes:          n,
		MediaType:      meta.MediaType,
		Name:           meta.Name,
		IntegrityBlock: IntegrityBlockBytes,
	}
	index := integrityIndex{Version: 1, Object: ref.Digest, Bytes: ref.Bytes, BlockBytes: IntegrityBlockBytes, Blocks: digests.finish()}
	indexBytes, err := json.Marshal(index)
	if err != nil {
		return Ref{}, fmt.Errorf("encode session content integrity index: %w", err)
	}
	indexSum := sha256.Sum256(indexBytes)
	ref.IndexDigest = hex.EncodeToString(indexSum[:])
	dest := s.objectPath(ref.Digest)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return Ref{}, fmt.Errorf("create session content object directory: %w", err)
	}
	if err := s.publishObject(ctx, tmpPath, dest, ref); err != nil {
		return Ref{}, err
	}
	if err := s.publishIndex(ctx, ref, indexBytes); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

// Open validates the complete immutable object before returning it positioned
// at byte zero. This favors integrity over trusting a mutable local filesystem.
func (s *Store) Open(ctx context.Context, ref Ref) (*os.File, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	f, err := s.openRaw(ref)
	if err != nil {
		return nil, err
	}
	if err := s.verifyOpenFile(ctx, f, ref); err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("rewind session content %s: %w", ref.Digest, err)
	}
	return f, nil
}

// Verify checks the immutable object and its block index without materializing
// the object. It is the explicit integrity boundary used by import/export and
// diagnostics.
func (s *Store) Verify(ctx context.Context, ref Ref) error {
	if err := validateRef(ref); err != nil {
		return err
	}
	f, err := s.openRaw(ref)
	if err != nil {
		return err
	}
	defer f.Close()
	return s.verifyOpenFile(ctx, f, ref)
}

// Stat validates the object and returns its caller-owned display metadata.
func (s *Store) Stat(ctx context.Context, ref Ref) (Ref, error) {
	if err := s.Verify(ctx, ref); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

// Probe opens the object through the bounded content root and validates its
// immutable integrity index without hashing the complete original.
func (s *Store) Probe(ctx context.Context, ref Ref) (ObjectWitness, error) {
	if err := validateRef(ref); err != nil {
		return ObjectWitness{}, err
	}
	if err := ctx.Err(); err != nil {
		return ObjectWitness{}, err
	}
	f, err := s.openRaw(ref)
	if err != nil {
		return ObjectWitness{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ObjectWitness{}, err
	}
	if ref.IndexDigest != "" {
		if _, err := s.readIndex(ctx, ref); err != nil {
			return ObjectWitness{}, err
		}
	}
	return ObjectWitness{info: info, bytes: info.Size(), modTimeNano: info.ModTime().UnixNano(), indexDigest: ref.IndexDigest}, nil
}

// ReadRange reads exactly length bytes starting at offset after validating the
// object. The per-call allocation is bounded independently of object size.
func (s *Store) ReadRange(ctx context.Context, ref Ref, offset, length int64) ([]byte, error) {
	if offset < 0 || length < 0 {
		return nil, errors.New("session content range must be non-negative")
	}
	if length > MaxReadRange {
		return nil, fmt.Errorf("session content range %d exceeds per-read budget %d", length, MaxReadRange)
	}
	if offset > ref.Bytes || length > ref.Bytes-offset {
		return nil, fmt.Errorf("session content range [%d,%d) exceeds object size %d", offset, offset+length, ref.Bytes)
	}
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	f, err := s.openRaw(ref)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if ref.IndexDigest == "" {
		if err := s.verifyOpenFile(ctx, f, ref); err != nil {
			return nil, err
		}
	} else if err := s.verifyRange(ctx, f, ref, offset, length); err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek session content %s: %w", ref.Digest, err)
	}
	buf := make([]byte, int(length))
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, fmt.Errorf("read session content %s range: %w", ref.Digest, err)
	}
	return buf, nil
}

func (s *Store) openRaw(ref Ref) (*os.File, error) {
	if s == nil || s.root == "" {
		return nil, errors.New("session content store unavailable")
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, fmt.Errorf("open session content root: %w", err)
	}
	defer root.Close()
	f, err := root.Open(s.objectRelativePath(ref.Digest))
	if err != nil {
		return nil, fmt.Errorf("open session content %s: %w", ref.Digest, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat session content %s: %w", ref.Digest, err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("session content %s is not a regular file", ref.Digest)
	}
	if info.Size() != ref.Bytes {
		_ = f.Close()
		return nil, fmt.Errorf("session content %s size is %d, expected %d", ref.Digest, info.Size(), ref.Bytes)
	}
	return f, nil
}

func (s *Store) verifyOpenFile(ctx context.Context, f *os.File, ref Ref) error {
	if ref.IndexDigest != "" {
		if _, err := s.readIndex(ctx, ref); err != nil {
			return err
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	digest := sha256.New()
	if _, err := copyWithContext(ctx, digest, f); err != nil {
		return fmt.Errorf("verify session content %s: %w", ref.Digest, err)
	}
	got := hex.EncodeToString(digest.Sum(nil))
	if got != ref.Digest {
		return fmt.Errorf("session content %s failed SHA-256 verification: got %s", ref.Digest, got)
	}
	return nil
}

func validateRef(ref Ref) error {
	if ref.Bytes < 0 {
		return errors.New("session content byte size must be non-negative")
	}
	if len(ref.Digest) != sha256.Size*2 {
		return fmt.Errorf("invalid session content digest %q", ref.Digest)
	}
	for _, c := range ref.Digest {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("invalid session content digest %q", ref.Digest)
		}
	}
	if ref.IndexDigest != "" {
		if len(ref.IndexDigest) != sha256.Size*2 || ref.IntegrityBlock != IntegrityBlockBytes {
			return fmt.Errorf("invalid session content integrity index for %q", ref.Digest)
		}
		for _, c := range ref.IndexDigest {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return fmt.Errorf("invalid session content index digest %q", ref.IndexDigest)
			}
		}
	}
	return nil
}

func (s *Store) objectPath(digest string) string {
	if len(digest) < 4 {
		return filepath.Join(s.root, "objects", digest)
	}
	return filepath.Join(s.root, "objects", digest[:2], digest[2:4], digest)
}

func (s *Store) objectRelativePath(digest string) string {
	if len(digest) < 4 {
		return filepath.Join("objects", digest)
	}
	return filepath.Join("objects", digest[:2], digest[2:4], digest)
}

func (s *Store) indexPath(digest string) string {
	if len(digest) < 4 {
		return filepath.Join(s.root, "indexes", digest+".json")
	}
	return filepath.Join(s.root, "indexes", digest[:2], digest[2:4], digest+".json")
}

func (s *Store) indexRelativePath(digest string) string {
	if len(digest) < 4 {
		return filepath.Join("indexes", digest+".json")
	}
	return filepath.Join("indexes", digest[:2], digest[2:4], digest+".json")
}

type blockDigestWriter struct {
	dst    io.Writer
	full   hash.Hash
	block  hash.Hash
	blockN int
	blocks []string
}

func newBlockDigestWriter(dst io.Writer) *blockDigestWriter {
	return &blockDigestWriter{dst: dst, full: sha256.New(), block: sha256.New()}
}

func (w *blockDigestWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		part := min(len(p), IntegrityBlockBytes-w.blockN)
		chunk := p[:part]
		n, err := w.dst.Write(chunk)
		if n > 0 {
			_, _ = w.full.Write(chunk[:n])
			_, _ = w.block.Write(chunk[:n])
			w.blockN += n
			written += n
			p = p[n:]
		}
		if err != nil {
			return written, err
		}
		if n != part {
			return written, io.ErrShortWrite
		}
		if w.blockN == IntegrityBlockBytes {
			w.blocks = append(w.blocks, hex.EncodeToString(w.block.Sum(nil)))
			w.block.Reset()
			w.blockN = 0
		}
	}
	return written, nil
}

func (w *blockDigestWriter) finish() []string {
	if w.blockN > 0 {
		w.blocks = append(w.blocks, hex.EncodeToString(w.block.Sum(nil)))
		w.block.Reset()
		w.blockN = 0
	}
	return append([]string(nil), w.blocks...)
}

func (s *Store) publishObject(ctx context.Context, tmpPath, dest string, ref Ref) error {
	if err := os.Link(tmpPath, dest); err == nil {
		_ = syncParent(filepath.Dir(dest))
		return nil
	} else if os.IsExist(err) {
		if verifyErr := s.Verify(ctx, Ref{Digest: ref.Digest, Bytes: ref.Bytes}); verifyErr != nil {
			return fmt.Errorf("existing session content %s is invalid: %w", ref.Digest, verifyErr)
		}
		return nil
	}
	release, err := filelock.Acquire(ctx, dest+".publish.lock")
	if err != nil {
		return fmt.Errorf("lock session content %s publication: %w", ref.Digest, err)
	}
	defer release()
	if _, err := os.Stat(dest); err == nil {
		return s.Verify(ctx, Ref{Digest: ref.Digest, Bytes: ref.Bytes})
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("publish session content %s: %w", ref.Digest, err)
	}
	return syncParent(filepath.Dir(dest))
}

func (s *Store) publishIndex(ctx context.Context, ref Ref, data []byte) error {
	dest := s.indexPath(ref.Digest)
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, ".tmp"), "index-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := writeAll(tmp, data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	release, err := filelock.Acquire(ctx, dest+".publish.lock")
	if err != nil {
		return err
	}
	defer release()
	if existing, err := os.ReadFile(dest); err == nil {
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("session content %s integrity index conflicts", ref.Digest)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return err
	}
	return syncParent(filepath.Dir(dest))
}

func (s *Store) readIndex(ctx context.Context, ref Ref) (integrityIndex, error) {
	if err := ctx.Err(); err != nil {
		return integrityIndex{}, err
	}
	if err := validateRef(ref); err != nil {
		return integrityIndex{}, err
	}
	if s == nil || s.root == "" {
		return integrityIndex{}, errors.New("session content store unavailable")
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return integrityIndex{}, fmt.Errorf("open session content root: %w", err)
	}
	defer root.Close()
	data, err := root.ReadFile(s.indexRelativePath(ref.Digest))
	if err != nil {
		return integrityIndex{}, fmt.Errorf("read session content %s integrity index: %w", ref.Digest, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != ref.IndexDigest {
		return integrityIndex{}, fmt.Errorf("session content %s integrity index failed SHA-256 verification", ref.Digest)
	}
	var index integrityIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return integrityIndex{}, err
	}
	wantBlocks := int((ref.Bytes + IntegrityBlockBytes - 1) / IntegrityBlockBytes)
	if index.Version != 1 || index.Object != ref.Digest || index.Bytes != ref.Bytes || index.BlockBytes != IntegrityBlockBytes || len(index.Blocks) != wantBlocks {
		return integrityIndex{}, fmt.Errorf("session content %s integrity index metadata mismatch", ref.Digest)
	}
	return index, nil
}

func (s *Store) verifyRange(ctx context.Context, f *os.File, ref Ref, offset, length int64) error {
	index, err := s.readIndex(ctx, ref)
	if err != nil {
		return err
	}
	if length == 0 {
		return nil
	}
	first := offset / IntegrityBlockBytes
	last := (offset + length - 1) / IntegrityBlockBytes
	buf := make([]byte, IntegrityBlockBytes)
	for block := first; block <= last; block++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := block * IntegrityBlockBytes
		size := min(IntegrityBlockBytes, ref.Bytes-start)
		if _, err := f.ReadAt(buf[:size], start); err != nil {
			return err
		}
		sum := sha256.Sum256(buf[:size])
		if hex.EncodeToString(sum[:]) != index.Blocks[block] {
			return fmt.Errorf("session content %s block %d failed SHA-256 verification", ref.Digest, block)
		}
	}
	return nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, copyBufferBytes)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			wn, writeErr := dst.Write(buf[:n])
			written += int64(wn)
			if writeErr != nil {
				return written, writeErr
			}
			if wn != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

func syncParent(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil && runtime.GOOS != "windows" && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOTSUP) && !errors.Is(err, syscall.ENOSYS) {
		return err
	}
	return nil
}
