package main

import (
	"crypto/rand"
	"encoding/hex"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type mediaTokenEntry struct {
	absPath   string
	identity  os.FileInfo
	filename  string
	mime      string
	kind      string
	size      int64
	modTime   time.Time
	createdAt time.Time
	expiresAt time.Time
}

type mediaTokenStore struct {
	mu    sync.Mutex
	byTok map[string]*mediaTokenEntry
	order []string
	maxN  int
	ttl   time.Duration
}

const mediaTokenMax = 256

func newMediaTokenStore() *mediaTokenStore {
	return &mediaTokenStore{
		byTok: map[string]*mediaTokenEntry{},
		maxN:  mediaTokenMax,
		ttl:   10 * time.Minute,
	}
}

func (s *mediaTokenStore) cleanupLocked() {
	now := time.Now()
	for len(s.order) > 0 {
		tok := s.order[0]
		e := s.byTok[tok]
		if e == nil {
			s.order = s.order[1:]
			continue
		}
		if !now.Before(e.expiresAt) {
			delete(s.byTok, tok)
			s.order = s.order[1:]
			continue
		}
		break
	}
	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}
}

func (s *mediaTokenStore) create(absPath, filename, mime, kind string, size int64, modTime time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()

	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	token := hex.EncodeToString(tok)
	now := time.Now()
	identity, _ := os.Stat(absPath)
	s.byTok[token] = &mediaTokenEntry{
		absPath: absPath, identity: identity, filename: filename, mime: mime, kind: kind,
		size: size, modTime: modTime, createdAt: now, expiresAt: now.Add(s.ttl),
	}
	s.order = append(s.order, token)

	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}
	return token
}

func (s *mediaTokenStore) get(token string) *mediaTokenEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byTok[token]
	if e == nil {
		return nil
	}
	if time.Now().After(e.expiresAt) {
		delete(s.byTok, token)
		return nil
	}
	return e
}

func (a *App) ensureMediaTokenStore() *mediaTokenStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mediaTokens == nil {
		a.mediaTokens = newMediaTokenStore()
	}
	return a.mediaTokens
}

// workspaceMediaMiddleware serves only files whose identity still matches the
// regular file authorized when the short-lived token was minted.
func (a *App) workspaceMediaMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "/__reasonix_workspace_media/"
			if !strings.HasPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, prefix), "/", 2)
			if len(parts) == 0 || parts[0] == "" {
				http.NotFound(w, r)
				return
			}
			entry := a.ensureMediaTokenStore().get(parts[0])
			if entry == nil {
				http.NotFound(w, r)
				return
			}

			f, err := os.Open(entry.absPath)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			opened, err := f.Stat()
			if err != nil || entry.identity == nil || !opened.Mode().IsRegular() || !os.SameFile(entry.identity, opened) {
				http.NotFound(w, r)
				return
			}

			w.Header().Set("Content-Type", entry.mime)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": entry.filename}))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, max-age=600")
			if entry.kind == "image" {
				w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
				w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
				w.Header().Set("Referrer-Policy", "no-referrer")
			}
			http.ServeContent(w, r, entry.filename, entry.modTime, f)
		})
	}
}
