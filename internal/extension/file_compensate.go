package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	// Prior bytes are an in-process recovery aid, not an unbounded file cache.
	// Keep large writes compensatable when practical while bounding one owner.
	defaultFilePriorMaxBytes      = 32 << 20
	defaultFilePriorMaxEntryBytes = 8 << 20
)

// FilePriorStore holds prior file contents for compensatable write_file
// receipts so recovery can restore them (never claim success without apply).
type FilePriorStore struct {
	mu            sync.Mutex
	byID          map[string]filePrior
	retainedBytes int
	maxBytes      int
	maxEntryBytes int
}

type filePrior struct {
	Path    string
	Content []byte
	Existed bool
}

// DefaultFilePriorStore belongs to the compatibility runtime owner.
var DefaultFilePriorStore = DefaultRuntimeOwner.FilePriors

// NewFilePriorStore returns an empty store.
func NewFilePriorStore() *FilePriorStore {
	return newFilePriorStore(defaultFilePriorMaxBytes, defaultFilePriorMaxEntryBytes)
}

func newFilePriorStore(maxBytes, maxEntryBytes int) *FilePriorStore {
	return &FilePriorStore{
		byID:          make(map[string]filePrior),
		maxBytes:      maxBytes,
		maxEntryBytes: maxEntryBytes,
	}
}

// Capture records prior content for path under receipt id.
// It returns false when retaining the prior would exceed the store budget.
func (s *FilePriorStore) Capture(id, path string, content []byte, existed bool) bool {
	if s == nil || id == "" || path == "" {
		return false
	}
	if s.maxEntryBytes > 0 && len(content) > s.maxEntryBytes {
		return false
	}
	s.mu.Lock()
	old, hadOld := s.byID[id]
	oldBytes := 0
	if hadOld {
		oldBytes = len(old.Content)
	}
	available := s.retainedBytes - oldBytes
	if s.maxBytes > 0 && available+len(content) > s.maxBytes {
		s.mu.Unlock()
		return false
	}
	cp := make([]byte, len(content))
	copy(cp, content)
	s.byID[id] = filePrior{Path: path, Content: cp, Existed: existed}
	s.retainedBytes = available + len(cp)
	s.mu.Unlock()
	return true
}

// Compensate restores the prior content (or removes a created file). Returns
// error if unknown or IO fails; updates receipt compensation status when store
// is DefaultReceiptStore-linked via caller.
func (s *FilePriorStore) Compensate(id string) error {
	if s == nil {
		return fmt.Errorf("extension: nil file prior store")
	}
	s.mu.Lock()
	prior, ok := s.byID[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("extension: no prior captured for %s", id)
	}
	if !prior.Existed {
		if err := os.Remove(prior.Path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(prior.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(prior.Path, prior.Content, 0o644)
}

// Forget drops a prior entry after successful compensation.
func (s *FilePriorStore) Forget(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if prior, ok := s.byID[id]; ok {
		s.retainedBytes -= len(prior.Content)
		if s.retainedBytes < 0 {
			s.retainedBytes = 0
		}
		delete(s.byID, id)
	}
	s.mu.Unlock()
}

// ApplyFileWriteCompensation restores prior content and marks the receipt applied.
func ApplyFileWriteCompensation(receiptID string) error {
	return RuntimeOwnerOrDefault(nil).ApplyFileWriteCompensation(receiptID)
}
