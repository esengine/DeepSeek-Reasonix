package checkpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"reasonix/internal/fileutil"
)

// BlobStore is a content-addressed store for checkpoint file payloads.
// Blobs are named by SHA-256 hex of their contents and written atomically.
type BlobStore struct {
	dir string
	mu  sync.Mutex
}

// NewBlobStore creates a blob store under dir (usually <session>.ckpt/blobs).
// An empty dir makes Put/Get operate as no-ops that return errors for Get.
func NewBlobStore(dir string) *BlobStore {
	return &BlobStore{dir: dir}
}

// Dir returns the blob directory.
func (b *BlobStore) Dir() string {
	if b == nil {
		return ""
	}
	return b.dir
}

// Put stores data if not already present and returns the content digest.
func (b *BlobStore) Put(data []byte) (string, error) {
	if b == nil || b.dir == "" {
		return "", fmt.Errorf("blob store unavailable")
	}
	sum := sha256.Sum256(data)
	ref := hex.EncodeToString(sum[:])
	path := b.path(ref)

	b.mu.Lock()
	defer b.mu.Unlock()
	if st, err := os.Stat(path); err == nil && st.Size() == int64(len(data)) {
		if existing, readErr := os.ReadFile(path); readErr == nil && Digest(existing) == ref {
			return ref, nil
		}
	}
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return "", err
	}
	if err := fileutil.AtomicWriteFileStrict(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write blob %s: %w", ref, err)
	}
	return ref, nil
}

// Get returns the blob bytes for ref.
func (b *BlobStore) Get(ref string) ([]byte, error) {
	if b == nil || b.dir == "" {
		return nil, fmt.Errorf("blob store unavailable")
	}
	if !validBlobRef(ref) {
		return nil, fmt.Errorf("invalid blob ref %q", ref)
	}
	data, err := os.ReadFile(b.path(ref))
	if err != nil {
		return nil, err
	}
	if got := Digest(data); got != ref {
		return nil, fmt.Errorf("blob %s failed content-address verification: got %s", ref, got)
	}
	return data, nil
}

// Has reports whether ref exists.
func (b *BlobStore) Has(ref string) bool {
	if b == nil || b.dir == "" || !validBlobRef(ref) {
		return false
	}
	_, err := b.Get(ref)
	return err == nil
}

// Remove deletes a blob. Missing refs are ignored.
func (b *BlobStore) Remove(ref string) error {
	if b == nil || b.dir == "" || !validBlobRef(ref) {
		return nil
	}
	err := os.Remove(b.path(ref))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Prune removes content-addressed blobs not present in live. It walks the
// fan-out tree and never treats directory names as blob references.
func (b *BlobStore) Prune(live map[string]struct{}) error {
	if b == nil || b.dir == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return filepath.WalkDir(b.dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ref := entry.Name()
		if !validBlobRef(ref) {
			return nil
		}
		if _, ok := live[ref]; ok {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}

// Size returns total bytes of all blobs.
func (b *BlobStore) Size() (int64, error) {
	if b == nil || b.dir == "" {
		return 0, nil
	}
	var total int64
	err := filepath.WalkDir(b.dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}

func (b *BlobStore) path(ref string) string {
	// Two-level fan-out to keep directory listings reasonable.
	if len(ref) < 4 {
		return filepath.Join(b.dir, ref)
	}
	return filepath.Join(b.dir, ref[:2], ref[2:4], ref)
}

func validBlobRef(ref string) bool {
	if len(ref) != 64 {
		return false
	}
	for _, c := range ref {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Digest returns the SHA-256 hex digest of data.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
