package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/fileutil"
)

// A persisted proof only skips re-launching `bash -c true` for an executable
// whose bytes are the ones that already ran; it decides nothing about the
// sandbox. Any doubt about the file, the store or its integrity is a miss.
const (
	shellProofVersion = 1
	shellProofTTL     = 7 * 24 * time.Hour
	shellProofMaxFile = 64 << 10
	shellProofMaxKeep = 16
)

type shellProof struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"mtime_ns"`
	Digest   string `json:"sha256"`
	ProvenAt int64  `json:"proven_at_ns"`
}

type shellProofFile struct {
	Version int          `json:"version"`
	Proofs  []shellProof `json:"proofs"`
	Sum     string       `json:"sum"`
}

type shellProofStore struct {
	mu  sync.Mutex
	dir string
	now func() time.Time
}

// newShellProofStore returns a store rooted at dir, or nil when dir is empty;
// a nil store holds and records nothing.
func newShellProofStore(dir string) *shellProofStore {
	if dir = strings.TrimSpace(dir); dir == "" {
		return nil
	}
	return &shellProofStore{dir: dir, now: time.Now}
}

func (s *shellProofStore) location() string {
	return filepath.Join(s.dir, "shell", "bash-proofs.json")
}

func digestFile(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", false
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

func proofSum(proofs []shellProof) string {
	b, _ := json.Marshal(proofs)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// plainFile reports whether p is a regular file that is not itself a link.
func plainFile(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

// plainDir reports whether p is a real directory, never a link to one.
func plainDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

func (s *shellProofStore) load() []shellProof {
	loc := s.location()
	if !plainDir(filepath.Dir(loc)) || !plainFile(loc) {
		return nil
	}
	f, err := os.Open(loc)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, shellProofMaxFile+1))
	if err != nil || len(b) > shellProofMaxFile {
		return nil
	}
	var file shellProofFile
	if json.Unmarshal(b, &file) != nil || file.Version != shellProofVersion || file.Sum != proofSum(file.Proofs) {
		return nil
	}
	return file.Proofs
}

func sameProofTarget(a, b shellProof) bool {
	return strings.EqualFold(a.Path, b.Path) && a.Size == b.Size && a.ModTime == b.ModTime
}

// holds reports whether a fresh proof matches the executable now at path:
// same path, size and mtime, and the same content digest.
func (s *shellProofStore) holds(path string, fi os.FileInfo) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	want := shellProof{Path: path, Size: fi.Size(), ModTime: fi.ModTime().UnixNano()}
	for _, p := range s.load() {
		if !sameProofTarget(p, want) || s.now().Sub(time.Unix(0, p.ProvenAt)) > shellProofTTL || p.ProvenAt > s.now().UnixNano() {
			continue
		}
		d, ok := digestFile(path)
		return ok && d == p.Digest
	}
	return false
}

// record keeps a successful probe under the digest taken before it ran, so a
// file swapped during the probe is never vouched for. Persistence is best effort: a store that
// cannot be written only costs the next launch one probe.
func (s *shellProofStore) record(path string, fi os.FileInfo, digest string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	loc := s.location()
	dir := filepath.Dir(loc)
	if err := os.MkdirAll(dir, 0o700); err != nil || !plainDir(dir) {
		return
	}
	if _, err := os.Lstat(loc); err == nil && !plainFile(loc) {
		return
	}
	now := s.now()
	entry := shellProof{Path: path, Size: fi.Size(), ModTime: fi.ModTime().UnixNano(), Digest: digest, ProvenAt: now.UnixNano()}
	keep := []shellProof{entry}
	for _, p := range s.load() {
		if !sameProofTarget(p, entry) && now.Sub(time.Unix(0, p.ProvenAt)) <= shellProofTTL && len(keep) < shellProofMaxKeep {
			keep = append(keep, p)
		}
	}
	b, err := json.Marshal(shellProofFile{Version: shellProofVersion, Proofs: keep, Sum: proofSum(keep)})
	if err != nil {
		return
	}
	_ = fileutil.AtomicWriteFile(loc, b, 0o600)
}
