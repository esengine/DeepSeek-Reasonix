package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
)

// Each refusal is a different next step: a malformed version is the caller's,
// an absent or unreachable object is the mirror's, and the kernel decides none
// of them from a message.
var (
	ErrNotesBadVersion  = errors.New("update: not a release version")
	ErrNotesAbsent      = errors.New("update: this release has no published notes")
	ErrNotesUnreachable = errors.New("update: the release notes could not be fetched")
	ErrNotesTooLarge    = errors.New("update: release notes exceed the allowed size")
)

// NotesMaxBytes bounds one notes document. The largest shipped is 40 KB.
const NotesMaxBytes = 256 << 10

// notesMaxVersion bounds the version string so a cache file name can never
// exceed what a file system accepts.
const notesMaxVersion = 64

const (
	notesTimeout = 15 * time.Second
	notesFailTTL = 60 * time.Second
)

// VersionNotes is one release's notes as the version panel shows them.
type VersionNotes struct {
	Version  string `json:"version"`
	Markdown string `json:"markdown"`
	Cached   bool   `json:"cached"`
}

var releaseVersion = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.]*)?)$`)

// notesURL is where a release's notes live. It is built from a validated
// version and the mirror constant, never from the catalog: the catalog only
// says whether notes exist, so it cannot aim this fetch anywhere.
func notesURL(v string) string { return StudioMirror + "/studio/notes/" + v + ".md" }

func notesVersion(version string) (string, error) {
	m := releaseVersion.FindStringSubmatch(strings.TrimSpace(version))
	if m == nil || len(m[1]) > notesMaxVersion {
		return "", ErrNotesBadVersion
	}
	return m[1], nil
}

// notesFileName is the cache name of a version. Prerelease identifiers are
// case-sensitive and a file system may not be, so a name that has capitals
// carries a digest of the exact spelling and two spellings never share a file.
func notesFileName(v string) string {
	lower := strings.ToLower(v)
	if lower == v {
		return v + ".md"
	}
	sum := sha256.Sum256([]byte(v))
	return lower + "-" + hex.EncodeToString(sum[:4]) + ".md"
}

type notesFlight struct {
	done chan struct{}
	res  VersionNotes
	err  error
}

type notesFailure struct {
	err   error
	until time.Time
}

// notesState owns what outlives one request: the fetches in flight, which a
// second click joins, and the recent failures, which it does not repeat.
type notesState struct {
	mu       sync.Mutex
	inflight map[string]*notesFlight
	failed   map[string]notesFailure
}

var sharedNotes = &notesState{}

// ReadNotes returns a release's notes from disk when they were ever fetched and
// from the mirror otherwise. A fetched document is kept for good: a released
// version's notes do not change. retry skips the short memory of a failure.
func ReadNotes(ctx context.Context, in Install, version string, retry bool) (VersionNotes, error) {
	client, err := netclient.NewHTTPClient(ProxySpec(), netclient.TransportOptions{})
	if err != nil {
		return VersionNotes{}, fmt.Errorf("%w: %w", ErrNotesUnreachable, err)
	}
	dir := ""
	if root := config.CacheDir(); root != "" {
		dir = filepath.Join(root, "release-notes")
	}
	return sharedNotes.read(ctx, notesRequest{dir: dir, client: client, userAgent: UserAgent(in.Version), retry: retry}, version)
}

// NotesReaderOver is ReadNotes with the route and the cache directory already
// decided, so a test drives the whole read without reaching the network. The
// address stays the constant; only how the request travels is chosen.
func NotesReaderOver(client *http.Client, dir string) func(context.Context, Install, string, bool) (VersionNotes, error) {
	st := &notesState{}
	return func(ctx context.Context, in Install, version string, retry bool) (VersionNotes, error) {
		return st.read(ctx, notesRequest{dir: dir, client: client, userAgent: UserAgent(in.Version), retry: retry}, version)
	}
}

type notesRequest struct {
	dir       string
	client    *http.Client
	userAgent string
	retry     bool
}

func (s *notesState) read(ctx context.Context, rq notesRequest, version string) (VersionNotes, error) {
	key, err := notesVersion(version)
	if err != nil {
		return VersionNotes{}, err
	}
	url := notesURL(key)
	if md, ok := readNotesFile(rq.dir, key); ok {
		return VersionNotes{Version: key, Markdown: md, Cached: true}, nil
	}

	s.mu.Lock()
	if f, ok := s.failed[key]; ok && !rq.retry && time.Now().Before(f.until) {
		s.mu.Unlock()
		return VersionNotes{}, f.err
	}
	fl, joined := s.inflight[key]
	if !joined {
		if s.inflight == nil {
			s.inflight, s.failed = map[string]*notesFlight{}, map[string]notesFailure{}
		}
		fl = &notesFlight{done: make(chan struct{})}
		s.inflight[key] = fl
		go s.fetch(rq, key, url, fl)
	}
	s.mu.Unlock()

	select {
	case <-fl.done:
		return fl.res, fl.err
	case <-ctx.Done():
		return VersionNotes{}, ctx.Err()
	}
}

// The fetch outlives the click that started it: a second click may be waiting
// on the same result, and abandoning it would fail both.
func (s *notesState) fetch(rq notesRequest, key, url string, fl *notesFlight) {
	ctx, cancel := context.WithTimeout(context.Background(), notesTimeout)
	defer cancel()
	md, err := fetchNotes(ctx, guarded(rq.client), url, rq.userAgent)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, key)
	if err != nil {
		s.failed[key] = notesFailure{err: err, until: time.Now().Add(notesFailTTL)}
		fl.err = err
	} else {
		delete(s.failed, key)
		// A document the disk would not take is still the answer; it is
		// fetched again next time.
		writeNotesFile(rq.dir, key, md)
		fl.res = VersionNotes{Version: key, Markdown: md}
	}
	close(fl.done)
}

func fetchNotes(ctx context.Context, c *http.Client, url, userAgent string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotesUnreachable, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/markdown, text/plain")
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotesUnreachable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNotesAbsent
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("%w: %s", ErrNotesUnreachable, resp.Status)
	case !documentType(resp.Header.Get("Content-Type")):
		return "", fmt.Errorf("%w: the mirror answered with content type %q", ErrNotesUnreachable, resp.Header.Get("Content-Type"))
	case resp.ContentLength > NotesMaxBytes:
		return "", ErrNotesTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, NotesMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotesUnreachable, err)
	}
	if len(body) > NotesMaxBytes {
		return "", ErrNotesTooLarge
	}
	if !usableNotes(body) {
		return "", fmt.Errorf("%w: the mirror answered something that is not a document", ErrNotesUnreachable)
	}
	return string(body), nil
}

// documentType judges the declared type, never the body: an error page served
// with a 200 must not be kept as a release's notes.
func documentType(header string) bool {
	typ, _, err := mime.ParseMediaType(header)
	return err == nil && (typ == "text/markdown" || typ == "text/plain")
}

func usableNotes(b []byte) bool {
	return len(strings.TrimSpace(string(b))) > 0 && utf8.Valid(b)
}

// A file that fails the same test as a fresh response is a miss, so a torn or
// emptied cache entry costs one fetch rather than a blank panel.
func readNotesFile(dir, key string) (string, bool) {
	if dir == "" {
		return "", false
	}
	f, err := os.Open(filepath.Join(dir, notesFileName(key)))
	if err != nil {
		return "", false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, NotesMaxBytes+1))
	if err != nil || len(b) > NotesMaxBytes || !usableNotes(b) {
		return "", false
	}
	return string(b), true
}

func writeNotesFile(dir, key, md string) {
	if dir == "" || os.MkdirAll(dir, 0o700) != nil {
		return
	}
	_ = fileutil.AtomicWriteFile(filepath.Join(dir, notesFileName(key)), []byte(md), 0o600)
}
