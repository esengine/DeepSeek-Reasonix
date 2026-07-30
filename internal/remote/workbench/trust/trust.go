// Package trust persists Provider Broker authorization scoped to Host identity
// and allowed provider refs. Records never store API keys, base URLs, headers,
// env names, or passwords.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

// Record is one durable authorization entry.
type Record struct {
	HostID              string    `json:"hostId"`
	KeyType             string    `json:"keyType,omitempty"`
	FingerprintSHA256   string    `json:"fingerprintSha256"`
	IdentityDigest      string    `json:"identityDigest,omitempty"` // Windows multi-key set digest
	AllowedProviderRefs []string  `json:"allowedProviderRefs"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// Store is a process-local durable trust store under Reasonix home.
type Store struct {
	path string
	mu   sync.Mutex
}

// DefaultStore returns the user trust store path.
func DefaultStore() *Store {
	dir := config.MemoryUserDir()
	if dir == "" {
		return &Store{path: ""}
	}
	return &Store{path: filepath.Join(dir, "remote-provider-trust.json")}
}

// NewStore uses an explicit path (tests).
func NewStore(path string) *Store { return &Store{path: path} }

type document struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

// Get returns the record for host+fingerprint.
func (s *Store) Get(hostID, fingerprint string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.loadLocked()
	if err != nil {
		return Record{}, false, err
	}
	hostID = strings.TrimSpace(hostID)
	fingerprint = strings.TrimSpace(fingerprint)
	for _, r := range doc.Records {
		if r.HostID == hostID && r.FingerprintSHA256 == fingerprint {
			return r, true, nil
		}
	}
	return Record{}, false, nil
}

// MissingRefs returns provider refs not yet authorized for this host identity.
func (s *Store) MissingRefs(hostID, fingerprint string, refs []string) ([]string, error) {
	rec, ok, err := s.Get(hostID, fingerprint)
	if err != nil {
		return nil, err
	}
	if !ok {
		out := uniqueSorted(refs)
		return out, nil
	}
	have := map[string]struct{}{}
	for _, r := range rec.AllowedProviderRefs {
		have[r] = struct{}{}
	}
	var missing []string
	for _, ref := range uniqueSorted(refs) {
		if _, ok := have[ref]; !ok {
			missing = append(missing, ref)
		}
	}
	return missing, nil
}

// AuthorizeAll merges refs into the durable record.
func (s *Store) AuthorizeAll(hostID, keyType, fingerprint string, refs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.loadLocked()
	if err != nil {
		return err
	}
	hostID = strings.TrimSpace(hostID)
	fingerprint = strings.TrimSpace(fingerprint)
	refs = uniqueSorted(refs)
	found := false
	for i, r := range doc.Records {
		if r.HostID == hostID && r.FingerprintSHA256 == fingerprint {
			set := map[string]struct{}{}
			for _, x := range r.AllowedProviderRefs {
				set[x] = struct{}{}
			}
			for _, x := range refs {
				set[x] = struct{}{}
			}
			merged := make([]string, 0, len(set))
			for x := range set {
				merged = append(merged, x)
			}
			sort.Strings(merged)
			r.AllowedProviderRefs = merged
			r.KeyType = keyType
			r.UpdatedAt = time.Now().UTC()
			doc.Records[i] = r
			found = true
			break
		}
	}
	if !found {
		doc.Records = append(doc.Records, Record{
			HostID:              hostID,
			KeyType:             keyType,
			FingerprintSHA256:   fingerprint,
			AllowedProviderRefs: refs,
			UpdatedAt:           time.Now().UTC(),
		})
	}
	return s.saveLocked(doc)
}

// IdentityDigest builds a stable digest over sorted algorithm+key material.
// Used on Windows where multiple host keys may be trusted.
func IdentityDigest(pairs []string) string {
	sorted := uniqueSorted(pairs)
	h := sha256.New()
	for _, p := range sorted {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Store) loadLocked() (document, error) {
	doc := document{Version: 1}
	if s.path == "" {
		return doc, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return doc, err
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("trust store corrupt: %w", err)
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	return doc, nil
}

func (s *Store) saveLocked(doc document) error {
	if s.path == "" {
		return fmt.Errorf("trust store path unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	doc.Version = 1
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, s.path)
}

func uniqueSorted(in []string) []string {
	set := map[string]struct{}{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
