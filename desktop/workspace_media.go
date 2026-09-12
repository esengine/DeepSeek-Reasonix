package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type mediaDependency struct {
	absPath  string
	identity os.FileInfo
	filename string
	mime     string
	modTime  time.Time
}

type mediaTokenEntry struct {
	absPath       string
	identity      os.FileInfo
	filename      string
	mime          string
	kind          string
	size          int64
	modTime       time.Time
	markdownImage bool
	createdAt     time.Time
	expiresAt     time.Time
	dependencies  map[string]mediaDependency
}

type mediaTokenStore struct {
	mu    sync.Mutex
	byTok map[string]*mediaTokenEntry
	order []string
	maxN  int
	ttl   time.Duration
}

const mediaTokenMax = 256

const (
	htmlPreviewDependencyMaxBytes = 4 << 20
	htmlPreviewBundleMaxBytes     = 32 << 20
	htmlPreviewDependencyMaxCount = 64
)

func newMediaTokenStore() *mediaTokenStore {
	return &mediaTokenStore{
		byTok: map[string]*mediaTokenEntry{},
		maxN:  mediaTokenMax,
		ttl:   10 * time.Minute,
	}
}

func (s *mediaTokenStore) cleanupLocked() {
	now := time.Now()
	active := s.order[:0]
	for _, tok := range s.order {
		e := s.byTok[tok]
		if e == nil {
			continue
		}
		if !now.Before(e.expiresAt) {
			delete(s.byTok, tok)
			continue
		}
		active = append(active, tok)
	}
	s.order = active
	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}
}

func (s *mediaTokenStore) extend(token string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	entry := s.byTok[token]
	if entry == nil {
		return false
	}
	entry.expiresAt = time.Now().Add(ttl)
	return true
}

func (s *mediaTokenStore) revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byTok, token)
}

func (s *mediaTokenStore) create(absPath, filename, mime, kind string, size int64, modTime time.Time) string {
	return s.createWithPolicy(absPath, filename, mime, kind, size, modTime, false, nil)
}

func (s *mediaTokenStore) createMarkdownImage(absPath, filename, mime string, identity os.FileInfo) string {
	return s.createWithPolicy(absPath, filename, mime, "image", identity.Size(), identity.ModTime(), true, identity)
}

func (s *mediaTokenStore) createHTML(absPath, allowedRoot, filename, mimeType string, identity os.FileInfo) (string, error) {
	dependencies, err := collectHTMLPreviewDependencies(absPath, allowedRoot, identity.Size())
	if err != nil {
		return "", err
	}
	token := s.createWithPolicy(absPath, filename, mimeType, "html", identity.Size(), identity.ModTime(), false, identity)
	s.mu.Lock()
	if entry := s.byTok[token]; entry != nil {
		entry.dependencies = dependencies
	}
	s.mu.Unlock()
	return token, nil
}

func collectHTMLPreviewDependencies(absPath, allowedRoot string, rootSize int64) (map[string]mediaDependency, error) {
	if rootSize > htmlPreviewBundleMaxBytes {
		return nil, errors.New("HTML preview exceeds the 32 MiB bundle budget")
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rootReal, err := filepath.EvalSymlinks(allowedRoot)
	if err != nil {
		return nil, err
	}
	rootReal, err = filepath.Abs(rootReal)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(absPath)
	dependencies := map[string]mediaDependency{}
	total := rootSize
	z := html.NewTokenizer(io.LimitReader(f, htmlPreviewBundleMaxBytes+1))
	for {
		tokenType := z.Next()
		if tokenType == html.ErrorToken {
			if errors.Is(z.Err(), io.EOF) {
				break
			}
			return nil, z.Err()
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		attrName := ""
		switch strings.ToLower(tok.Data) {
		case "script", "img", "source", "audio", "video":
			attrName = "src"
		case "link":
			attrName = "href"
		default:
			continue
		}
		for _, attr := range tok.Attr {
			if strings.ToLower(attr.Key) != attrName {
				continue
			}
			ref, parseErr := url.Parse(strings.TrimSpace(attr.Val))
			if parseErr != nil {
				return nil, errors.New("HTML preview contains an invalid local resource URL")
			}
			if ref.Scheme != "" || ref.Host != "" || ref.Path == "" || strings.HasPrefix(ref.Path, "//") {
				continue
			}
			if strings.HasPrefix(ref.Path, "/") {
				return nil, errors.New("HTML preview contains an unsupported root-relative local resource: " + ref.Path)
			}
			decoded, decodeErr := url.PathUnescape(ref.Path)
			if decodeErr != nil {
				return nil, errors.New("HTML preview contains an invalid escaped resource path")
			}
			key := filepath.ToSlash(filepath.Clean(filepath.FromSlash(decoded)))
			if key == "." || key == ".." || strings.HasPrefix(key, "../") || filepath.IsAbs(key) {
				return nil, errors.New("HTML preview resource leaves the document directory")
			}
			if _, exists := dependencies[key]; exists {
				continue
			}
			if len(dependencies) >= htmlPreviewDependencyMaxCount {
				return nil, errors.New("HTML preview references more than 64 local resources")
			}
			candidate := filepath.Join(baseDir, filepath.FromSlash(key))
			finalInfo, lstatErr := os.Lstat(candidate)
			if lstatErr != nil {
				return nil, errors.New("HTML preview resource is missing: " + key)
			}
			if finalInfo.Mode()&os.ModeSymlink != 0 || !finalInfo.Mode().IsRegular() {
				return nil, errors.New("HTML preview resource is not a regular file: " + key)
			}
			candidateReal, evalErr := filepath.EvalSymlinks(candidate)
			if evalErr != nil {
				return nil, evalErr
			}
			candidateReal, evalErr = filepath.Abs(candidateReal)
			if evalErr != nil {
				return nil, evalErr
			}
			relative, relErr := filepath.Rel(rootReal, candidateReal)
			if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, errors.New("HTML preview resource leaves the authorized root: " + key)
			}
			if finalInfo.Size() > htmlPreviewDependencyMaxBytes {
				return nil, errors.New("HTML preview resource exceeds 4 MiB: " + key)
			}
			total += finalInfo.Size()
			if total > htmlPreviewBundleMaxBytes {
				return nil, errors.New("HTML preview bundle exceeds 32 MiB")
			}
			depMIME := mime.TypeByExtension(strings.ToLower(filepath.Ext(candidateReal)))
			if depMIME == "" {
				depMIME = "application/octet-stream"
			}
			dependencies[key] = mediaDependency{absPath: candidateReal, identity: finalInfo, filename: filepath.Base(candidateReal), mime: depMIME, modTime: finalInfo.ModTime()}
		}
	}
	return dependencies, nil
}

func (s *mediaTokenStore) createWithPolicy(absPath, filename, mime, kind string, size int64, modTime time.Time, markdownImage bool, identity os.FileInfo) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()

	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	token := hex.EncodeToString(tok)
	now := time.Now()
	if identity == nil {
		identity, _ = os.Stat(absPath)
	}
	s.byTok[token] = &mediaTokenEntry{
		absPath: absPath, identity: identity, filename: filename, mime: mime, kind: kind,
		size: size, modTime: modTime, markdownImage: markdownImage, createdAt: now, expiresAt: now.Add(s.ttl),
	}
	s.order = append(s.order, token)

	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}
	return token
}

func readValidatedMarkdownImageSnapshot(f *os.File, mimeType string, size int64) ([]byte, error) {
	if size <= 0 {
		return nil, errors.New("empty markdown image")
	}
	if size > remoteMarkdownImageMaxBytes {
		return nil, errMarkdownImageTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(f, remoteMarkdownImageMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > remoteMarkdownImageMaxBytes {
		return nil, errMarkdownImageTooLarge
	}
	snapshot, detected := safeRemoteMarkdownImage(body)
	if detected == "" || detected != mimeType {
		return nil, errors.New("markdown image MIME does not match the authorized file")
	}
	if err := validateMarkdownImageBytes(snapshot, detected); err != nil {
		return nil, err
	}
	return snapshot, nil
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

			path := entry.absPath
			identity := entry.identity
			filename := entry.filename
			mimeType := entry.mime
			modTime := entry.modTime
			isRoot := true
			if len(parts) == 2 {
				requested, unescapeErr := url.PathUnescape(parts[1])
				if unescapeErr != nil {
					http.NotFound(w, r)
					return
				}
				requested = filepath.ToSlash(filepath.Clean(filepath.FromSlash(requested)))
				if requested != entry.filename {
					dependency, found := entry.dependencies[requested]
					if !found {
						http.NotFound(w, r)
						return
					}
					path, identity, filename, mimeType, modTime = dependency.absPath, dependency.identity, dependency.filename, dependency.mime, dependency.modTime
					isRoot = false
				}
			}

			f, err := os.Open(path)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			opened, err := f.Stat()
			if err != nil || identity == nil || !opened.Mode().IsRegular() || !os.SameFile(identity, opened) {
				http.NotFound(w, r)
				return
			}
			content := io.ReadSeeker(f)
			if entry.markdownImage {
				snapshot, snapshotErr := readValidatedMarkdownImageSnapshot(f, entry.mime, opened.Size())
				if snapshotErr != nil {
					if errors.Is(snapshotErr, errMarkdownImageTooLarge) {
						http.Error(w, "markdown image exceeds the decode budget", http.StatusRequestEntityTooLarge)
					} else {
						http.Error(w, "markdown image is invalid", http.StatusUnsupportedMediaType)
					}
					return
				}
				content = bytes.NewReader(snapshot)
			}

			w.Header().Set("Content-Type", mimeType)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filename}))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, max-age=600")
			if entry.kind == "image" {
				w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
				w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
				w.Header().Set("Referrer-Policy", "no-referrer")
			} else if entry.kind == "html" && isRoot {
				w.Header().Set("Content-Security-Policy", "default-src 'self' https: data: blob:; script-src 'self' https: 'unsafe-inline'; style-src 'self' https: 'unsafe-inline'; img-src 'self' https: data: blob:; media-src 'self' https: data: blob:; connect-src https:; frame-src https:; object-src 'none'; base-uri 'none'; form-action 'none'; sandbox allow-scripts")
				w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
				w.Header().Set("Referrer-Policy", "no-referrer")
			}
			http.ServeContent(w, r, filename, modTime, content)
		})
	}
}
