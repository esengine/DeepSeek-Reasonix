package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
)

const (
	fileIndexFormat          = 1
	FileExpirySeconds        = 7 * 24 * 3600
	FileRefreshMarginSeconds = 3600
	defaultFileIndexName     = "files-v1.json"
)

// FileIndexRecord is one durable Files API mapping. It never stores the API key.
type FileIndexRecord struct {
	Scope     string `json:"scope"`
	Variant   string `json:"variant"`
	FileID    string `json:"fileId"`
	Bytes     int    `json:"bytes"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
}

type storedFileIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Records       []FileIndexRecord `json:"records"`
}

// FileIndex reuses official DeepSeek file ids across sessions in one home.
type FileIndex struct {
	path string
	mu   sync.Mutex
}

var (
	defaultFileIndex   *FileIndex
	defaultFileIndexMu sync.Mutex
)

// DefaultFileIndex is the process-wide reuse index. Tests may replace it.
func DefaultFileIndex() *FileIndex {
	defaultFileIndexMu.Lock()
	defer defaultFileIndexMu.Unlock()
	if defaultFileIndex != nil {
		return defaultFileIndex
	}
	defaultFileIndex = NewFileIndex(defaultFileIndexPath())
	return defaultFileIndex
}

func SetDefaultFileIndex(idx *FileIndex) {
	defaultFileIndexMu.Lock()
	defaultFileIndex = idx
	defaultFileIndexMu.Unlock()
}

func NewFileIndex(path string) *FileIndex {
	return &FileIndex{path: strings.TrimSpace(path)}
}

func defaultFileIndexPath() string {
	home := strings.TrimSpace(os.Getenv("REASONIX_HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, "llm-deepseek", defaultFileIndexName)
}

// FileScope is a non-secret namespace: SHA-256(endpoint + key). Messages
// scopes include a /v1 suffix so they never share Chat Completions ids.
func FileScope(baseURL, apiKey, protocol string) string {
	root := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.EqualFold(strings.TrimSpace(protocol), "anthropic") {
		root += "/v1"
	}
	sum := sha256.Sum256([]byte(root + "\x00" + apiKey))
	return hex.EncodeToString(sum[:])
}

// FileVariant is the request-version identity (SHA-256 of uploaded bytes).
func FileVariant(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (idx *FileIndex) Lookup(scope, variant string, now, marginMs int64) (FileIndexRecord, bool) {
	if idx == nil || idx.path == "" || scope == "" || variant == "" {
		return FileIndexRecord{}, false
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	store, err := idx.loadLocked()
	if err != nil {
		return FileIndexRecord{}, false
	}
	for _, rec := range store.Records {
		if rec.Scope == scope && rec.Variant == variant && rec.ExpiresAt-now > marginMs {
			return rec, true
		}
	}
	return FileIndexRecord{}, false
}

func (idx *FileIndex) BytesForFileID(fileID string) (int, bool) {
	fileID = strings.TrimSpace(fileID)
	if idx == nil || idx.path == "" || fileID == "" {
		return 0, false
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	store, err := idx.loadLocked()
	if err != nil {
		return 0, false
	}
	for _, rec := range store.Records {
		if rec.FileID == fileID {
			return rec.Bytes, true
		}
	}
	return 0, false
}

func (idx *FileIndex) Commit(rec FileIndexRecord, now, marginMs int64) (FileIndexRecord, bool) {
	if idx == nil || idx.path == "" || rec.Scope == "" || rec.Variant == "" || rec.FileID == "" {
		return rec, false
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(idx.path), 0o700); err != nil {
		return rec, false
	}
	release, err := filelock.Acquire(context.Background(), idx.path+".lock")
	if err != nil {
		return rec, false
	}
	defer release()
	store, err := idx.loadLocked()
	if err != nil {
		store = storedFileIndex{FormatVersion: fileIndexFormat}
	}
	for _, existing := range store.Records {
		if existing.Scope == rec.Scope && existing.Variant == rec.Variant && existing.ExpiresAt-now > marginMs {
			return existing, false
		}
	}
	kept := make([]FileIndexRecord, 0, len(store.Records)+1)
	for _, existing := range store.Records {
		if existing.ExpiresAt-now > marginMs && !(existing.Scope == rec.Scope && existing.Variant == rec.Variant) {
			kept = append(kept, existing)
		}
	}
	kept = append(kept, rec)
	store.FormatVersion = fileIndexFormat
	store.Records = kept
	raw, err := json.Marshal(store)
	if err != nil {
		return rec, false
	}
	if err := fileutil.AtomicWriteFile(idx.path, append(raw, '\n'), 0o600); err != nil {
		return rec, false
	}
	return rec, true
}

func (idx *FileIndex) loadLocked() (storedFileIndex, error) {
	raw, err := os.ReadFile(idx.path)
	if err != nil {
		if os.IsNotExist(err) {
			return storedFileIndex{FormatVersion: fileIndexFormat}, nil
		}
		return storedFileIndex{}, err
	}
	var store storedFileIndex
	if json.Unmarshal(raw, &store) != nil || store.FormatVersion != fileIndexFormat {
		return storedFileIndex{FormatVersion: fileIndexFormat}, nil
	}
	if store.Records == nil {
		store.Records = []FileIndexRecord{}
	}
	return store, nil
}

func FileRefreshMarginMs() int64 {
	return int64(FileRefreshMarginSeconds) * int64(time.Second/time.Millisecond)
}

func FileExpiryDeadline(nowMs int64) int64 {
	return nowMs + int64(FileExpirySeconds)*1000
}
