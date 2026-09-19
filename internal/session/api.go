package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// VisitCommits streams the complete durable commit prefix from one canonical
// session directory. It is intended for exports and diagnostics that must not
// materialize the cumulative event log.
func VisitCommits(ctx context.Context, dir string, visit func(Commit) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	file, err := os.Open(logPathForManifest(dir, manifest))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	var visitErr error
	adapter := func(_ int64, commit Commit) bool {
		if visit != nil {
			visitErr = visit(commit)
		}
		return visitErr == nil
	}
	if manifest.Codec == Codec {
		err = scanV4CommitFile(ctx, file, 0, 1, contentStoreForSessionDir(dir), nil, adapter)
	} else {
		err = scanCommitFileCodec(file, 0, 1, manifest.Codec, nil, adapter)
	}
	return errors.Join(err, visitErr)
}

// AccessMode separates cold readers from the single leased writer. Read-only
// access never repairs, migrates, truncates, or advances writer generation.
type AccessMode string

const (
	ReadOnly  AccessMode = "read"
	ReadWrite AccessMode = "write"
)

type CreateOptions struct {
	SessionID       string
	CWD             string
	ParentSessionID string
	Origin          SessionOrigin
}

type SessionInfo struct {
	SessionID       string
	Ref             SessionRef
	Codec           string
	Title           string
	TitleSequence   uint64
	ModelRef        string
	ModelIdentity   string
	Turns           int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	EventSequence   uint64
	ResultSequence  uint64
	Preview         string
	MetadataStatus  string
	CWD             string
	ParentSessionID string
	Origin          SessionOrigin
	Path            string
	Error           string
}

type SessionPage struct {
	Sessions   []SessionInfo
	NextCursor string
}

type EventPage struct {
	Commits   []Commit
	Next      uint64
	Truncated bool
}

// eventPageReader is the paged durable read surface shared by the physical
// handle and a cold Session, so catalog rebuilds never depend on replaying a
// whole in-memory log.
type eventPageReader interface {
	Read(context.Context, uint64, int) (EventPage, error)
}

// SessionHandle is the physical persistence contract. It reads and writes
// bytes for one session identity and owns the writer lease; it holds no
// projection, operation table, or accepted commit list.
type SessionHandle interface {
	ID() string
	Manifest() Manifest
	Read(context.Context, uint64, int) (EventPage, error)
	Append(context.Context, []Commit) error
	Sync(context.Context) (DurableReceipt, error)
	Close(context.Context) error
}

// WritableSessionHandle is the leased physical handle. Byte-level maintenance
// operations such as fork, export, and interrupted-turn recovery are Session
// operations because they read or extend the in-memory log; this contract only
// distinguishes a leased writer from a cold reader.
type WritableSessionHandle interface {
	SessionHandle
	SessionID() string
}

type SessionPersistence interface {
	Create(CreateOptions) (*Session, error)
	Open(sessionID string, mode AccessMode) (*Session, error)
	Stat(context.Context, string) (SessionInfo, error)
	List(context.Context, string, int) (SessionPage, error)
}

// FilesystemPersistence owns a versioned sessions-v4 root.
type FilesystemPersistence struct{ Root string }

func NewFilesystemPersistence(root string) *FilesystemPersistence {
	return &FilesystemPersistence{Root: filepath.Clean(root)}
}

// RootForLegacyDir maps a host's legacy transcript catalog to the sibling
// final-format store. The mapping lives in the persistence package so Boot and
// controllers never derive a v3 identity from a transcript path.
func RootForLegacyDir(sessionDir string) string {
	dir := filepath.Clean(strings.TrimSpace(sessionDir))
	if dir == "." || dir == "" {
		return ""
	}
	if filepath.Base(dir) == "sessions" {
		return filepath.Join(filepath.Dir(dir), "sessions-v4")
	}
	return filepath.Join(dir, "sessions-v4")
}

func (p *FilesystemPersistence) Create(options CreateOptions) (*Session, error) {
	id := strings.TrimSpace(options.SessionID)
	if id == "" {
		id = randomID()
	}
	if err := validateSessionID(id); err != nil {
		return nil, err
	}
	dir, err := p.sessionDir(id, false)
	if err != nil {
		return nil, err
	}
	options.SessionID = id
	header, err := headerForCreate(options)
	if err != nil {
		return nil, err
	}
	return createWithOptions(dir, id, OpenOptions{ExternalHistory: true}, header)
}

func (p *FilesystemPersistence) Open(sessionID string, mode AccessMode) (*Session, error) {
	id := strings.TrimSpace(sessionID)
	if err := validateSessionID(id); err != nil {
		return nil, err
	}
	dir, err := p.sessionDir(id, true)
	if err != nil {
		return nil, err
	}
	if _, _, err := readSessionHeader(dir, id); err != nil {
		return nil, err
	}
	if mode == ReadOnly {
		return openReadSession(dir, id, filepath.Join(p.Root, ".query-cache", filepath.Base(id)))
	}
	if mode != ReadWrite {
		return nil, fmt.Errorf("session: unsupported access mode %q", mode)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	} else if err != nil {
		return nil, err
	}
	return OpenWithOptions(dir, id, OpenOptions{ExternalHistory: true})
}

func (p *FilesystemPersistence) Stat(ctx context.Context, sessionID string) (SessionInfo, error) {
	if err := ctx.Err(); err != nil {
		return SessionInfo{}, err
	}
	id := strings.TrimSpace(sessionID)
	if err := validateSessionID(id); err != nil {
		return SessionInfo{}, err
	}
	dir, err := p.sessionDir(id, true)
	if err != nil {
		return SessionInfo{}, err
	}
	manifest, err := readCatalogManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return SessionInfo{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
		}
		return SessionInfo{}, err
	}
	if manifest.SessionID != id {
		return SessionInfo{}, fmt.Errorf("%w: manifest belongs to %q", ErrDamagedStore, manifest.SessionID)
	}
	header, hasHeader, err := readSessionHeader(dir, id)
	if err != nil {
		return SessionInfo{}, err
	}
	// Listing reads the manifest, log metadata, and rebuildable catalog cache.
	// It never opens event bodies; Query refreshes missing display metadata in
	// the background.
	revision, err := revisionOfLog(dir)
	if err != nil {
		return SessionInfo{}, err
	}
	updatedAt := manifest.CreatedAt
	if revision.Exists {
		if stat, statErr := os.Stat(logPathForManifest(dir, manifest)); statErr == nil && stat.ModTime().After(updatedAt) {
			updatedAt = stat.ModTime()
		}
	}
	info := SessionInfo{SessionID: manifest.SessionID, Codec: manifest.Codec, CreatedAt: manifest.CreatedAt, UpdatedAt: updatedAt, MetadataStatus: MetadataPending, Path: dir}
	if hasHeader {
		info.CWD, info.ParentSessionID, info.Origin = header.CWD, header.ParentSessionID, header.Origin
	}
	cacheDir := filepath.Join(p.Root, ".query-cache", filepath.Base(id))
	if metadata, metadataErr := readCatalogMetadata(cacheDir, manifest, revision); metadataErr == nil {
		info.Title, info.TitleSequence = metadata.Title, metadata.TitleSequence
		info.ModelRef, info.ModelIdentity = metadata.ModelRef, metadata.ModelIdentity
		info.Turns, info.Preview, info.MetadataStatus = metadata.Turns, metadata.Preview, MetadataReady
		info.EventSequence, info.ResultSequence = metadata.Sequence, metadata.ResultSequence
	}
	return info, nil
}

func readCatalogManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if !supportedStoredManifest(manifest) {
		return Manifest{}, fmt.Errorf("%w: manifest schema or codec", ErrUnsupportedVersion)
	}
	return manifest, nil
}

func (p *FilesystemPersistence) sessionDir(id string, mustExist bool) (string, error) {
	if err := validateSessionID(id); err != nil {
		return "", err
	}
	if !mustExist {
		if err := os.MkdirAll(p.Root, 0o700); err != nil {
			return "", err
		}
	}
	root, err := os.OpenRoot(p.Root)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	if err != nil {
		return "", err
	}
	defer root.Close()
	// Root.Lstat rejects traversal and follows the platform's reparse-point
	// boundary rules. The single-segment validation above also keeps lock and
	// cache names portable on Windows.
	info, err := root.Lstat(id)
	if os.IsNotExist(err) && !mustExist {
		return filepath.Join(p.Root, id), nil
	}
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("session: session identity %q is not a confined directory", id)
	}
	// Resolve the physical path through the rooted handle. This keeps protocol
	// input out of the path propagated through the storage stack.
	child, err := root.OpenRoot(id)
	if err != nil {
		return "", err
	}
	dir := child.Name()
	if err := child.Close(); err != nil {
		return "", err
	}
	return dir, nil
}

func (p *FilesystemPersistence) List(ctx context.Context, cursor string, limit int) (SessionPage, error) {
	if err := ctx.Err(); err != nil {
		return SessionPage{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return SessionPage{}, fmt.Errorf("session: list limit must be 1..100")
	}
	entries, err := os.ReadDir(p.Root)
	if os.IsNotExist(err) {
		return SessionPage{Sessions: []SessionInfo{}}, nil
	}
	if err != nil {
		return SessionPage{}, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return SessionPage{}, err
		}
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && entry.Name() > cursor {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	page := SessionPage{Sessions: []SessionInfo{}}
	for _, id := range ids {
		info, statErr := p.Stat(ctx, id)
		if statErr != nil {
			info = SessionInfo{SessionID: id, Path: filepath.Join(p.Root, id), Error: statErr.Error()}
		}
		if len(page.Sessions) == limit {
			page.NextCursor = page.Sessions[len(page.Sessions)-1].SessionID
			break
		}
		page.Sessions = append(page.Sessions, info)
	}
	return page, nil
}

func validateSessionID(id string) error {
	id = strings.TrimSpace(id)
	if len(id) == 0 || len(id) > 255 || strings.HasPrefix(id, ".") || strings.HasSuffix(id, ".") ||
		!filepath.IsLocal(id) || id == "." || filepath.Base(id) != id || strings.ContainsAny(id, `/\\<>:"|?*`) {
		return fmt.Errorf("session: invalid session id %q", id)
	}
	for _, char := range id {
		if char < 0x20 {
			return fmt.Errorf("session: invalid session id %q", id)
		}
	}
	base := strings.ToUpper(strings.SplitN(id, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
		(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
		return fmt.Errorf("session: reserved session id %q", id)
	}
	return nil
}

// ValidateSessionID applies the canonical session storage identity rules.
// Callers that accept compatibility routes must validate the extracted ID
// before deciding whether the input names a SessionRef or a legacy path.
func ValidateSessionID(id string) error {
	return validateSessionID(id)
}

type readHandle struct {
	id       string
	dir      string
	cacheDir string
	manifest Manifest
	mu       sync.Mutex
	closed   bool
}

// openReadSession returns a cold Session backed only by the durable prefix. It
// performs no replay and takes no writer lease: paged reads remain the only way
// to consume it, which is what keeps catalog and history queries cheap.
func openReadSession(dir, id string, cacheDirs ...string) (*Session, error) {
	handle, err := openReadHandle(dir, id, cacheDirs...)
	if err != nil {
		return nil, err
	}
	return newReadSession(handle), nil
}

func openReadHandle(dir, id string, cacheDirs ...string) (*readHandle, error) {
	cacheDir := dir
	if len(cacheDirs) > 0 && strings.TrimSpace(cacheDirs[0]) != "" {
		cacheDir = cacheDirs[0]
	}
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
		}
		return nil, err
	}
	if manifest.SessionID != id {
		return nil, fmt.Errorf("session: manifest belongs to %q", manifest.SessionID)
	}
	return &readHandle{id: id, dir: dir, cacheDir: cacheDir, manifest: manifest}, nil
}

func (h *readHandle) ID() string { return h.id }

func (h *readHandle) Manifest() Manifest { return h.manifest }

// Dir reports the directory this cold reader opened. A fork from a session with
// no live runtime still has to locate the parent's owned files, and that must
// not require acquiring the writer lease the cold reader deliberately avoids.
func (h *readHandle) Dir() string {
	if h == nil {
		return ""
	}
	return h.dir
}

func (h *readHandle) Read(ctx context.Context, offset uint64, limit int) (EventPage, error) {
	if h == nil {
		return EventPage{}, os.ErrClosed
	}
	h.mu.Lock()
	closed := h.closed
	dir, cacheDir := h.dir, h.cacheDir
	h.mu.Unlock()
	if closed {
		return EventPage{}, os.ErrClosed
	}
	return readCommitPageWithCache(ctx, dir, cacheDir, offset, limit)
}

func (h *readHandle) Append(context.Context, []Commit) error {
	return ErrReadOnly
}

func (h *readHandle) Sync(context.Context) (DurableReceipt, error) {
	if h == nil {
		return DurableReceipt{}, os.ErrClosed
	}
	h.mu.Lock()
	closed := h.closed
	dir := h.dir
	h.mu.Unlock()
	if closed {
		return DurableReceipt{}, os.ErrClosed
	}
	sequence, err := lastDurableSequence(dir)
	return DurableReceipt{DurableSequence: sequence}, err
}

func (h *readHandle) Close(context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	return nil
}

func (s *Store) Read(ctx context.Context, offset uint64, limit int) (EventPage, error) {
	if s == nil {
		return EventPage{}, os.ErrClosed
	}
	s.mu.Lock()
	closed := s.closed
	dir := s.dir
	s.mu.Unlock()
	if closed {
		return EventPage{}, os.ErrClosed
	}
	return readCommitPage(ctx, dir, offset, limit)
}

func readCommitPage(ctx context.Context, dir string, offset uint64, limit int) (EventPage, error) {
	return readCommitPageWithCache(ctx, dir, dir, offset, limit)
}

func readCommitPageWithCache(ctx context.Context, dir, cacheDir string, offset uint64, limit int) (EventPage, error) {
	if err := ctx.Err(); err != nil {
		return EventPage{}, err
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return EventPage{}, fmt.Errorf("session: read limit must be 1..1000 commits")
	}
	index, err := loadOrBuildSparseIndex(ctx, dir, cacheDir)
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{Commits: []Commit{}}
	if index.LastSequence <= offset {
		return page, nil
	}
	checkpoint := index.checkpoint(offset)
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return EventPage{}, err
	}
	file, err := os.Open(logPathForManifest(dir, manifest))
	if err != nil {
		return EventPage{}, err
	}
	defer file.Close()
	visit := func(_ int64, commit Commit) bool {
		if ctx.Err() != nil {
			return false
		}
		if commit.LastSequence() <= offset {
			return true
		}
		if len(page.Commits) == limit {
			page.Truncated = true
			return false
		}
		page.Commits = append(page.Commits, commit)
		page.Next = commit.LastSequence()
		return true
	}
	if manifest.Codec == Codec {
		err = scanV4CommitFile(ctx, file, checkpoint.Offset, checkpoint.FirstSequence, contentStoreForSessionDir(dir), nil, visit)
	} else {
		err = scanCommitFileCodec(file, checkpoint.Offset, checkpoint.FirstSequence, manifest.Codec, nil, visit)
	}
	if err != nil {
		return EventPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return EventPage{}, err
	}
	return page, nil
}

func lastDurableSequence(dir string) (uint64, error) {
	return lastDurableSequenceWithCache(dir, dir)
}

func lastDurableSequenceWithCache(dir, cacheDir string) (uint64, error) {
	index, err := loadOrBuildSparseIndex(context.Background(), dir, cacheDir)
	return index.LastSequence, err
}

var _ SessionPersistence = (*FilesystemPersistence)(nil)
var _ SessionHandle = (*Store)(nil)
var _ SessionHandle = (*readHandle)(nil)
