package mcpauth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const storeVersion = 1

// credentialsFile holds all stored OAuth credentials, keyed by server origin.
type credentialsFile struct {
	Version     int                          `json:"version"`
	Credentials map[string]*StoredCredential `json:"credentials"`
}

// Store persists OAuth credentials per MCP server origin in a JSON file with
// 0600 permissions. It is safe for concurrent use; a single Store is shared
// across all remote MCP servers in a process.
type Store struct {
	path string

	mu     sync.Mutex
	loaded bool
	creds  map[string]*StoredCredential
}

// NewStore opens (creating the parent dir, not the file) the credentials store
// at path. The file is created lazily on the first write.
func NewStore(path string) *Store {
	return &Store{path: path, creds: map[string]*StoredCredential{}}
}

// Get returns the credential stored for origin, or (nil, false) when none.
func (s *Store) Get(origin string) (*StoredCredential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadOnce(); err != nil {
		return nil, false
	}
	c, ok := s.creds[origin]
	return c, ok
}

// Put stores cred for origin and persists immediately. It writes the file
// atomically with 0600 permissions so a crash mid-write cannot corrupt or
// truncate the existing store.
func (s *Store) Put(origin string, cred *StoredCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadOnce(); err != nil {
		return err
	}
	if cred != nil {
		cred.IssuedAt = time.Now()
	}
	s.creds[origin] = cred
	return s.flushLocked()
}

// Delete removes the credential for origin, if present, and persists.
func (s *Store) Delete(origin string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadOnce(); err != nil {
		return err
	}
	if _, ok := s.creds[origin]; !ok {
		return nil
	}
	delete(s.creds, origin)
	return s.flushLocked()
}

// loadOnce reads the file the first time any accessor runs. Missing file is
// not an error: the store starts empty. Caller holds s.mu.
func (s *Store) loadOnce() error {
	if s.loaded {
		return nil
	}
	s.loaded = true
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		// A corrupt store must not block authorization forever; treat it as
		// empty so the user can re-authenticate. The next Put overwrites it.
		return nil
	}
	if cf.Credentials != nil {
		s.creds = cf.Credentials
	}
	return nil
}

// flushLocked serializes and atomically writes the store. Caller holds s.mu.
func (s *Store) flushLocked() error {
	cf := credentialsFile{Version: storeVersion, Credentials: s.creds}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".mcp-oauth-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if any step below fails.
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	return nil
}
