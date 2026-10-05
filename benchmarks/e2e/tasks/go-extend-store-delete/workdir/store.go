package store

import (
	"errors"
	"sync"
)

// ErrNotFound is returned by every lookup that names a key the store does not
// hold. Callers compare with errors.Is, so it must stay a single sentinel.
var ErrNotFound = errors.New("store: key not found")

type Store struct {
	mu sync.RWMutex
	m  map[string]string
}

func New() *Store {
	return &Store{m: map[string]string{}}
}

func (s *Store) Put(key, value string) error {
	if key == "" {
		return errors.New("store: empty key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

func (s *Store) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}
