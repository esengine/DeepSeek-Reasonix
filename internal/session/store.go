// Package session owns Reasonix's canonical session service.
//
// Runtime owns live business state and activity authority, PersistenceBinding
// owns accepted work and durability progress, Store owns framed bytes and the
// writer lease, and Query owns rebuildable disk projections. A commit is
// accepted before it is durable; Flush establishes a semantic checkpoint.
package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
)

const (
	SchemaVersion = V4SchemaVersion
	// StorageRevision distinguishes the final v4 layout from unpublished v4
	// drafts. Physical layout changes are migration boundaries even when the
	// logical codec remains v4.
	StorageRevision = 2
	// Codec identifies the current framed linear session format. Earlier linear
	// and prototype stores are immutable migration inputs.
	Codec             = V4Codec
	FinalV31Codec     = "reasonix.session.linear/v3.1"
	LegacyLinearCodec = "reasonix.session.linear/v3"
	PrototypeCodec    = "reasonix.session.events/v3"
	LiveBatchDelay    = 200 * time.Millisecond
	currentLogName    = "events.frames"
	legacyLogName     = "events.jsonl"
)

var (
	ErrUnsupportedVersion   = errors.New("unsupported session storage version")
	ErrDamagedStore         = errors.New("damaged session store")
	ErrStaleGeneration      = errors.New("stale session writer generation")
	ErrOperationConflict    = errors.New("session operation id conflicts with an earlier batch")
	ErrPersistenceUncertain = errors.New("session persistence result is uncertain")
	ErrSessionNotFound      = errors.New("session not found")
	ErrSessionExists        = errors.New("session already exists")
	ErrWriterOwned          = errors.New("session writer is owned by another runtime")
	ErrReadOnly             = errors.New("session handle is read-only")
)

type Manifest struct {
	SchemaVersion    int       `json:"schemaVersion"`
	Codec            string    `json:"codec"`
	StorageRevision  int       `json:"storageRevision,omitempty"`
	ContentRoot      string    `json:"contentRoot,omitempty"`
	SessionID        string    `json:"sessionId"`
	CreatedAt        time.Time `json:"createdAt"`
	WriterGeneration uint64    `json:"writerGeneration"`
	InheritedEvents  uint64    `json:"inheritedEventCount,omitempty"`
	Source           *Source   `json:"source,omitempty"`
}

const sharedContentRoot = "../.content-v1"

type Event struct {
	ID         string              `json:"id"`
	Sequence   uint64              `json:"seq"`
	Kind       string              `json:"kind"`
	Optional   bool                `json:"optional,omitempty"`
	Required   bool                `json:"required,omitempty"`
	Payload    json.RawMessage     `json:"payload,omitempty"`
	PayloadRef *sessioncontent.Ref `json:"payloadRef,omitempty"`
}

type Commit struct {
	SchemaVersion    int       `json:"schemaVersion"`
	Codec            string    `json:"codec"`
	RecordType       string    `json:"recordType"`
	ID               string    `json:"commitId"`
	OperationID      string    `json:"operationId"`
	OperationHash    string    `json:"operationHash"`
	FirstSequence    uint64    `json:"firstSeq"`
	EventCount       int       `json:"eventCount"`
	TurnID           string    `json:"turnId,omitempty"`
	WriterGeneration uint64    `json:"writerGeneration"`
	CreatedAt        time.Time `json:"createdAt"`
	Events           []Event   `json:"events"`
}

func (c Commit) LastSequence() uint64 {
	if c.EventCount == 0 {
		return c.FirstSequence
	}
	return c.FirstSequence + uint64(c.EventCount) - 1
}

type Batch struct {
	OperationID string
	TurnID      string
	Events      []Event
}

type DurableReceipt struct {
	DurableSequence uint64 `json:"durableSequence"`
}

type PersistenceStatus string

const (
	PersistenceReady     PersistenceStatus = "ready"
	PersistencePending   PersistenceStatus = "pending"
	PersistenceFailed    PersistenceStatus = "failed"
	PersistenceUncertain PersistenceStatus = "uncertain"
)

type Snapshot struct {
	EventSequence     uint64
	DurableSequence   uint64
	PersistenceStatus PersistenceStatus
	PersistenceError  string
	Projection        Projection
}

type timerHandle interface{ Stop() bool }

type OpenOptions struct {
	AfterFunc func(time.Duration, func()) timerHandle
	Write     func(context.Context, io.Writer, []byte) error
	Sync      func(*os.File) error
	// ExternalHistory keeps durable UI messages out of the live projection.
	// Production SessionService enables it; low-level compatibility callers
	// retain the historical full-projection behavior unless requested.
	ExternalHistory bool
	// ObserveRecovery receives bounded open-path I/O counters after recovery.
	// Capacity tests use it to distinguish checkpoint recovery from prefix replay.
	ObserveRecovery func(RecoveryOpenStats)
	// DisableRecoveryPublish is a fault-injection hook used to verify that a
	// durable log tail is replayed from the previous checkpoint.
	DisableRecoveryPublish bool
}

type operationRecord struct {
	hash   string
	commit Commit
}

func compactOperationRecord(commit Commit) operationRecord {
	metadata := commit
	metadata.Events = nil
	return operationRecord{hash: commit.OperationHash, commit: metadata}
}

type uncertainWrite struct {
	start       int64
	stagedPath  string
	stagedBytes int64
	commitCount int
}

type uncertainAppendError struct {
	cause error
	write uncertainWrite
}

func (e *uncertainAppendError) Error() string {
	return fmt.Sprintf("%v: append at offset %d may have changed the log: %v", ErrPersistenceUncertain, e.write.start, e.cause)
}

func (e *uncertainAppendError) Unwrap() error { return ErrPersistenceUncertain }

// Store is the physical framed-log handle for one session directory. It owns the
// writer lease, the open file, and the rebuildable sparse offset index. It
// deliberately holds no projection, operation table, or accepted commit list:
// those belong to Session.
type Store struct {
	mu           sync.Mutex
	indexMu      sync.Mutex
	closeOnce    sync.Once
	closeErr     error
	dir          string
	manifest     Manifest
	file         *os.File
	releaseLease func()
	closed       bool
	index        sparseIndex
	content      *sessioncontent.Store
	startup      *startupSessionState
	recovery     *recoveryStore
	identity     storageIdentity
	tip          durableTip

	writeFn func(context.Context, io.Writer, []byte) error
	syncFn  func(*os.File) error
}

type startupSessionState struct {
	projection     Projection
	operations     map[string]operationRecord
	durable        uint64
	catalogPreview string
	recentMessages []provider.Message
	tip            durableTip
}

type durableTip struct {
	LogOffset      int64
	AnchorOffset   int64
	AnchorFirst    uint64
	AnchorCommitID string
	AnchorHash     string
}

// ID returns the immutable session identity of the physical store.
func (s *Store) ID() string {
	return s.SessionID()
}

func (s *Store) SessionID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.manifest.SessionID
}

func (s *Store) Manifest() Manifest {
	if s == nil {
		return Manifest{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest := s.manifest
	if manifest.Source != nil {
		source := *manifest.Source
		manifest.Source = &source
	}
	return manifest
}

// Dir reports the confined directory that backs this handle.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dir
}

// writableFile returns the open append file for durability repair. The caller
// must already hold the writer lease, which the handle acquired at open.
func (s *Store) writableFile() (*os.File, error) {
	if s == nil {
		return nil, os.ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return nil, os.ErrClosed
	}
	return s.file, nil
}

// Open opens an existing legacy-compatible path, creating it when absent. It
// returns a live Session: the in-memory log is the caller's business state.
func Open(dir, sessionID string) (*Session, error) {
	return OpenWithOptions(dir, sessionID, OpenOptions{})
}

// OpenWithOptions is the low-level test/import constructor retained while the
// old controller adapter is removed. Production callers use
// FilesystemPersistence, whose Open is strict and never creates a session.
func OpenWithOptions(dir, sessionID string, opts OpenOptions) (*Session, error) {
	if _, err := os.Stat(filepath.Clean(strings.TrimSpace(dir))); os.IsNotExist(err) {
		return CreateWithOptions(dir, sessionID, opts)
	}
	handle, err := openExistingHandle(dir, sessionID, opts)
	if err != nil {
		return nil, err
	}
	return bindSession(handle, opts)
}

func CreateStore(dir, sessionID string) (*Session, error) {
	return CreateWithOptions(dir, sessionID, OpenOptions{})
}

func CreateWithOptions(dir, sessionID string, opts OpenOptions) (*Session, error) {
	return createWithOptions(dir, sessionID, opts, nil)
}

func createWithOptions(dir, sessionID string, opts OpenOptions, header *SessionHeader) (*Session, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	sessionID = strings.TrimSpace(sessionID)
	if dir == "." || sessionID == "" {
		return nil, fmt.Errorf("session: directory and session id are required")
	}
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrSessionExists, sessionID)
		}
		return nil, err
	}
	created := true
	defer func() {
		if created {
			_ = os.RemoveAll(dir)
		}
	}()
	createdAt := time.Now().UTC()
	if header != nil {
		header.SessionID = sessionID
		header.CreatedAt = createdAt
	}
	manifest := Manifest{SchemaVersion: SchemaVersion, Codec: Codec, StorageRevision: StorageRevision, ContentRoot: sharedContentRoot, SessionID: sessionID, CreatedAt: createdAt}
	if err := writeManifestFile(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		return nil, err
	}
	if header != nil {
		if err := writeSessionHeader(dir, *header); err != nil {
			return nil, err
		}
	}
	if err := fileutil.AtomicWriteFileStrict(filepath.Join(dir, currentLogName), nil, 0o600); err != nil {
		return nil, err
	}
	created = false
	return OpenWithOptions(dir, sessionID, opts)
}

func openExistingHandle(dir, sessionID string, opts OpenOptions) (*Store, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	sessionID = strings.TrimSpace(sessionID)
	if dir == "." || sessionID == "" {
		return nil, fmt.Errorf("session: directory and session id are required")
	}
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("session: session path is not a directory: %s", dir)
	}
	eventsPath := filepath.Join(dir, currentLogName)
	releaseLease, err := acquireSessionWriter(dir)
	if err != nil {
		if errors.Is(err, filelock.ErrHeld) {
			return nil, fmt.Errorf("%w: %s", ErrWriterOwned, sessionID)
		}
		return nil, err
	}
	fail := func(err error) (*Store, error) {
		releaseLease()
		return nil, err
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return fail(err)
	}
	if manifest.SessionID != sessionID {
		return fail(fmt.Errorf("session: manifest belongs to %q", manifest.SessionID))
	}
	identity, err := ensureStorageIdentity(dir, manifest)
	if err != nil {
		return fail(err)
	}
	recovery, err := openRecoveryStoreRepair(dir, identity)
	if err != nil {
		return fail(err)
	}
	failRecovery := func(err error) (*Store, error) {
		_ = recovery.close()
		return fail(err)
	}
	// Build runtime state in one streaming validation pass. A newer required
	// event or a damaged complete batch leaves the original tail untouched.
	startup, durableEnd, torn, stats, usedCheckpoint, err := loadStartupSessionState(context.Background(), dir, eventsPath, opts.ExternalHistory, recovery, identity)
	if opts.ObserveRecovery != nil {
		opts.ObserveRecovery(stats)
	}
	if err != nil {
		return failRecovery(err)
	}
	if opts.ExternalHistory && startup.catalogPreview == "" {
		revision, revisionErr := revisionOfLog(dir)
		cacheDir := filepath.Join(filepath.Dir(dir), ".query-cache", filepath.Base(dir))
		if revisionErr == nil {
			if metadata, metadataErr := readCatalogMetadata(cacheDir, manifest, revision); metadataErr == nil {
				startup.catalogPreview = metadata.Preview
			}
		}
	}
	if torn {
		// Cold readers deliberately stop at the last complete record. A writer
		// may repair that tail only after acquiring the exclusive lease above:
		// preserve the original bytes first, then truncate back to the durable
		// commit boundary. This never invents or partially replays an event.
		if _, err := preserveAndTruncateTail(eventsPath, durableEnd, "torn"); err != nil {
			return failRecovery(fmt.Errorf("recover torn v3 tail: %w", err))
		}
	}
	if !usedCheckpoint {
		checkpoint := checkpointFromStartup(manifest, identity, startup)
		if err := recovery.publish(context.Background(), checkpoint, startup.operations); err == nil {
			startup.operations = map[string]operationRecord{}
		}
	}
	// Upgrade only after the exclusive writer validated the complete log. Old
	// readers reject revision 2 before using caches or accepting new writes.
	manifest.StorageRevision = StorageRevision
	manifest.WriterGeneration++
	if err := writeManifestFile(manifestPath, manifest); err != nil {
		return failRecovery(err)
	}
	f, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return failRecovery(err)
	}
	writeFn := opts.Write
	if writeFn == nil {
		writeFn = writeAllContext
	}
	syncFn := opts.Sync
	if syncFn == nil {
		syncFn = func(file *os.File) error { return file.Sync() }
	}
	// Opening a runtime must not rebuild a historical seek index. The writer
	// needs only the durable sequence and byte end; readers maintain their own
	// disposable locator in the query cache.
	index := sparseIndex{Codec: sparseIndexCodec, LogSize: durableEnd, LastSequence: startup.durable, partial: startup.durable > 0}
	return &Store{
		dir: dir, manifest: manifest, file: f, releaseLease: releaseLease,
		index: index, content: contentStoreForSessionDir(dir), startup: startup,
		recovery: recovery, identity: identity, tip: startup.tip,
		writeFn: writeFn, syncFn: syncFn,
	}, nil
}

func loadStartupSessionState(ctx context.Context, dir, eventsPath string, externalHistory bool, recovery *recoveryStore, identity storageIdentity) (*startupSessionState, int64, bool, RecoveryOpenStats, bool, error) {
	file, err := os.Open(eventsPath)
	if os.IsNotExist(err) {
		projection, _ := Project(nil)
		return &startupSessionState{projection: projection, operations: map[string]operationRecord{}}, 0, false, RecoveryOpenStats{}, false, nil
	}
	if err != nil {
		return nil, 0, false, RecoveryOpenStats{}, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, false, RecoveryOpenStats{}, false, err
	}
	if state, end, torn, stats, ok := loadRecoveryStartupState(ctx, dir, file, info, recovery, identity); ok {
		return state, end, torn, stats, true, nil
	}
	stats := RecoveryOpenStats{LogBytesTotal: info.Size()}
	if externalHistory {
		state, end, torn, err := loadBoundedStartupSessionState(ctx, dir, file, info)
		stats.LogBytesRead = end
		return state, end, torn, stats, false, err
	}
	projection, _ := Project(nil)
	state := &startupSessionState{projection: projection, operations: map[string]operationRecord{}}
	var durableEnd int64
	var projectionErr error
	err = scanV4CommitFile(ctx, file, 0, 1, contentStoreForSessionDir(dir), nil, func(offset int64, commit Commit) bool {
		if applyErr := applyProjectionCommit(&state.projection, commit); applyErr != nil {
			projectionErr = applyErr
			return false
		}
		if externalHistory {
			for _, message := range state.projection.Messages {
				if state.catalogPreview == "" {
					state.catalogPreview = catalogMessagePreview(message)
				}
			}
			state.projection.Messages = nil
		}
		state.operations[commit.OperationID] = compactOperationRecord(commit)
		state.durable = commit.LastSequence()
		durableEnd, _ = file.Seek(0, io.SeekCurrent)
		state.tip = durableTip{LogOffset: durableEnd, AnchorOffset: offset, AnchorFirst: commit.FirstSequence, AnchorCommitID: commit.ID, AnchorHash: commit.OperationHash}
		if err := applyRecentCommit(&state.recentMessages, commit); err != nil {
			projectionErr = err
			return false
		}
		return true
	})
	if err != nil {
		return nil, 0, false, stats, false, err
	}
	if projectionErr != nil {
		return nil, 0, false, stats, false, projectionErr
	}
	stats.LogBytesRead = durableEnd
	return state, durableEnd, durableEnd < info.Size(), stats, false, nil
}

// loadBoundedStartupSessionState separates lightweight business recovery from
// model-context recovery. The first pass validates every transaction without
// resolving historical message bodies and locates the newest context reset.
// The second pass materializes only the provider workset after that reset.
func loadBoundedStartupSessionState(ctx context.Context, dir string, file *os.File, info os.FileInfo) (*startupSessionState, int64, bool, error) {
	projection, _ := Project(nil)
	state := &startupSessionState{projection: projection, operations: map[string]operationRecord{}}
	modelOffset, modelSequence := int64(0), uint64(1)
	var durableEnd int64
	var projectionErr error
	sawModelEvent := false
	content := contentStoreForSessionDir(dir)
	err := scanV4CommitFileRefs(ctx, file, 0, 1, content, nil, func(offset int64, commit Commit) bool {
		business := commit
		business.Events = nil
		for _, event := range commit.Events {
			if modelProjectionEvent(event.Kind) {
				sawModelEvent = true
				if modelProjectionReset(event.Kind) {
					modelOffset, modelSequence = offset, commit.FirstSequence
				}
				continue
			}
			resolved, err := resolveProjectionEvent(ctx, content, event)
			if err != nil {
				projectionErr = err
				return false
			}
			business.Events = append(business.Events, resolved)
		}
		if err := applyProjectionCommit(&state.projection, business); err != nil {
			projectionErr = err
			return false
		}
		recent := commit
		recent.Events = nil
		for _, event := range commit.Events {
			switch event.Kind {
			case "message/complete", "message/upsert", "message/retract", "history/replace", "legacy/import":
				resolved, err := resolveProjectionEvent(ctx, content, event)
				if err != nil {
					projectionErr = err
					return false
				}
				recent.Events = append(recent.Events, resolved)
				applyTranscriptMetadata(&state.projection, commit, resolved)
			}
		}
		if err := applyRecentCommit(&state.recentMessages, recent); err != nil {
			projectionErr = err
			return false
		}
		state.operations[commit.OperationID] = compactOperationRecord(commit)
		state.durable = commit.LastSequence()
		durableEnd, _ = file.Seek(0, io.SeekCurrent)
		state.tip = durableTip{LogOffset: durableEnd, AnchorOffset: offset, AnchorFirst: commit.FirstSequence, AnchorCommitID: commit.ID, AnchorHash: commit.OperationHash}
		return true
	})
	if err != nil {
		return nil, 0, false, err
	}
	if projectionErr != nil {
		return nil, 0, false, projectionErr
	}
	if sawModelEvent {
		inputs, hidden, retracted := state.projection.TranscriptInputs, state.projection.HiddenTurns, state.projection.RetractedInputs
		if err := loadCurrentModelProjection(ctx, file, content, state, modelOffset, modelSequence); err != nil {
			return nil, 0, false, err
		}
		state.projection.TranscriptInputs, state.projection.HiddenTurns = inputs, hidden
		state.projection.RetractedInputs = retracted
	}
	state.projection.Messages = nil
	state.projection.CommittedSequence = state.durable
	return state, durableEnd, durableEnd < info.Size(), nil
}

func loadCurrentModelProjection(ctx context.Context, file *os.File, content *sessioncontent.Store, state *startupSessionState, offset int64, sequence uint64) error {
	var projectionErr error
	err := scanV4CommitFileRefs(ctx, file, offset, sequence, content, nil, func(_ int64, commit Commit) bool {
		model := commit
		model.Events = nil
		for _, event := range commit.Events {
			if !modelProjectionEvent(event.Kind) {
				continue
			}
			resolved, err := resolveProjectionEvent(ctx, content, event)
			if err != nil {
				projectionErr = err
				return false
			}
			model.Events = append(model.Events, resolved)
		}
		if err := applyProjectionCommit(&state.projection, model); err != nil {
			projectionErr = err
			return false
		}
		return true
	})
	if err != nil {
		return err
	}
	return projectionErr
}

func resolveProjectionEvent(ctx context.Context, content *sessioncontent.Store, event Event) (Event, error) {
	if event.PayloadRef == nil {
		return event, nil
	}
	payload, err := resolveContentPayload(ctx, content, *event.PayloadRef)
	if err != nil {
		return Event{}, fmt.Errorf("%w: read v4 event %s payload: %w", ErrDamagedStore, event.ID, err)
	}
	event.Payload, event.PayloadRef = payload, nil
	return event, nil
}

func modelProjectionEvent(kind string) bool {
	switch kind {
	case "message/complete", "message/upsert", "message/retract", "history/replace", "model/context-replace", "compaction", "legacy/import":
		return true
	default:
		return false
	}
}

func modelProjectionReset(kind string) bool {
	switch kind {
	case "history/replace", "model/context-replace", "compaction", "legacy/import":
		return true
	default:
		return false
	}
}

// bindSession replays the durable prefix and constructs the in-memory Session
// over a binding for the exact handle.
func bindSession(handle *Store, opts OpenOptions) (*Session, error) {
	dir := handle.Dir()
	state := handle.startup
	if state == nil {
		projection, _ := Project(nil)
		state = &startupSessionState{projection: projection, operations: map[string]operationRecord{}}
	}
	handle.startup = nil
	binding := newPersistenceBinding(handle, dir, state.durable, opts)
	session := newSession(handle.Manifest().SessionID, handle.Manifest(), nil, state.projection, binding)
	session.next = state.durable + 1
	session.operations = state.operations
	session.externalHistory = opts.ExternalHistory
	session.catalogPreview = state.catalogPreview
	session.recentMessages = detachMessages(state.recentMessages)
	session.durableRecent = detachMessages(state.recentMessages)
	session.storageGeneration = handle.identity.Generation
	session.recovery = handle.recovery
	binding.metadataSource = session.metadataForDurable
	binding.recoverySource = session.recoveryForDurable
	binding.recoveryPublished = session.recoveryPublished
	binding.disableRecoveryPublish = opts.DisableRecoveryPublish
	return session, nil
}

func writeManifestFile(path string, manifest Manifest) error {
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, append(b, '\n'), 0o600)
}

func contentStoreForSessionDir(dir string) *sessioncontent.Store {
	root := filepath.Join(filepath.Dir(dir), ".content-v1")
	if data, err := os.ReadFile(filepath.Join(dir, "manifest.json")); err == nil {
		var location struct {
			ContentRoot string `json:"contentRoot"`
		}
		if json.Unmarshal(data, &location) == nil && strings.TrimSpace(location.ContentRoot) != "" {
			candidate := filepath.Clean(filepath.Join(dir, location.ContentRoot))
			relative, relErr := filepath.Rel(dir, candidate)
			if relErr == nil && (relative == ".content-v1" || relative == sharedContentRoot) {
				root = candidate
			}
		}
	}
	return sessioncontent.New(root)
}

func logPathForManifest(dir string, manifest Manifest) string {
	if manifest.Codec == Codec {
		return filepath.Join(dir, currentLogName)
	}
	return filepath.Join(dir, legacyLogName)
}

func supportedStoredManifest(manifest Manifest) bool {
	if currentStoredManifest(manifest) {
		return true
	}
	return manifest.SchemaVersion == 3 &&
		(manifest.Codec == FinalV31Codec || manifest.Codec == LegacyLinearCodec || manifest.Codec == PrototypeCodec)
}

func currentStoredManifest(manifest Manifest) bool {
	return manifest.SchemaVersion == SchemaVersion && manifest.Codec == Codec &&
		(manifest.StorageRevision == 1 || manifest.StorageRevision == StorageRevision)
}

func readStoredManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return Manifest{}, err
	}
	if !supportedStoredManifest(manifest) {
		return Manifest{}, fmt.Errorf("%w: manifest schema or codec", ErrUnsupportedVersion)
	}
	return manifest, nil
}

// Append writes already-committed batches in order. Sequence allocation,
// validation, and idempotency belong to Session; this method is only the
// physical hand-off and reports uncertainty rather than guessing.
func (s *Store) Append(ctx context.Context, commits []Commit) error {
	if s == nil {
		return fmt.Errorf("session: nil store")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(commits) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return os.ErrClosed
	}
	s.indexMu.Lock()
	next := s.index.LastSequence + 1
	s.indexMu.Unlock()
	for i, commit := range commits {
		if commit.SchemaVersion != SchemaVersion || commit.Codec != Codec || commit.RecordType != "commit" ||
			commit.ID == "" || commit.OperationID == "" || commit.OperationHash == "" ||
			commit.WriterGeneration != s.manifest.WriterGeneration || commit.FirstSequence != next ||
			commit.EventCount == 0 || commit.EventCount != len(commit.Events) {
			return fmt.Errorf("%w: invalid physical commit %d at sequence %d", ErrDamagedStore, i, next)
		}
		for eventIndex, event := range commit.Events {
			want := commit.FirstSequence + uint64(eventIndex)
			if event.Sequence != want || event.ID == "" || strings.TrimSpace(event.Kind) == "" {
				return fmt.Errorf("%w: invalid physical event at sequence %d", ErrDamagedStore, want)
			}
		}
		next = commit.LastSequence() + 1
	}
	return s.persist(ctx, s.file, commits)
}

func (s *Store) persist(ctx context.Context, file *os.File, commits []Commit) error {
	staged, err := os.CreateTemp(s.dir, ".append-*.staged")
	if err != nil {
		return fmt.Errorf("stage v4 append: %w", err)
	}
	stagedPath := staged.Name()
	keepStaged := false
	defer func() {
		_ = staged.Close()
		if !keepStaged {
			_ = os.Remove(stagedPath)
		}
	}()
	lengths, err := encodeV4Commits(ctx, staged, s.content, commits)
	if err != nil {
		return err
	}
	if err := staged.Sync(); err != nil {
		return fmt.Errorf("fsync staged v4 append: %w", err)
	}
	stagedInfo, err := staged.Stat()
	if err != nil {
		return err
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return err
	}
	start, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if err := copyStagedAppend(ctx, staged, file, s.writeFn); err != nil {
		end, statErr := file.Seek(0, io.SeekEnd)
		if statErr == nil && end == start {
			return err
		}
		if statErr == nil && end == start+stagedInfo.Size() {
			if syncErr := s.syncFn(file); syncErr == nil {
				s.recordPersistedIndex(file, start, commits, lengths)
				return nil
			}
		}
		keepStaged = true
		return &uncertainAppendError{cause: err, write: uncertainWrite{start: start, stagedPath: stagedPath, stagedBytes: stagedInfo.Size(), commitCount: len(commits)}}
	}
	if err := s.syncFn(file); err != nil {
		keepStaged = true
		return &uncertainAppendError{cause: fmt.Errorf("fsync: %w", err), write: uncertainWrite{start: start, stagedPath: stagedPath, stagedBytes: stagedInfo.Size(), commitCount: len(commits)}}
	}
	s.recordPersistedIndex(file, start, commits, lengths)
	return nil
}

func copyStagedAppend(ctx context.Context, source io.Reader, destination io.Writer, writeFn func(context.Context, io.Writer, []byte) error) error {
	buffer := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			if err := writeFn(ctx, destination, buffer[:n]); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// Sync fsyncs the physical log and reports the durable sequence observed on
// disk. It is the handle half of a semantic checkpoint; PersistenceBinding
// pairs it with queue drain.
func (s *Store) Sync(ctx context.Context) (DurableReceipt, error) {
	if s == nil {
		return DurableReceipt{}, os.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return DurableReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return DurableReceipt{}, os.ErrClosed
	}
	if err := s.syncFn(s.file); err != nil {
		return DurableReceipt{}, err
	}
	s.indexMu.Lock()
	sequence := s.index.LastSequence
	s.indexMu.Unlock()
	return DurableReceipt{DurableSequence: sequence}, nil
}

func (s *Store) Close(_ context.Context) error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		file := s.file
		recovery := s.recovery
		s.file = nil
		s.recovery = nil
		s.closed = true
		releaseLease := s.releaseLease
		s.releaseLease = nil
		s.mu.Unlock()
		var closeErr error
		if file != nil {
			closeErr = file.Close()
		}
		if recovery != nil {
			closeErr = errors.Join(closeErr, recovery.close())
		}
		if releaseLease != nil {
			releaseLease()
		}
		s.closeErr = closeErr
	})
	return s.closeErr
}

func writeAllContext(ctx context.Context, w io.Writer, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// Replay returns the complete durable prefix. It ignores only an unterminated
// final record; a later write owner preserves and repairs that tail after it
// acquires the exclusive lease.
func Replay(dir string, knownKinds map[string]bool) ([]Commit, error) {
	commits := []Commit{}
	err := scanDurableCommits(dir, knownKinds, func(commit Commit) bool {
		commits = append(commits, commit)
		return true
	})
	return commits, err
}

// scanDurableCommits validates records in sequence and lets paged readers stop
// without materializing the rest of a large log. The next page resumes from a
// sequence cursor; a rebuildable offset index can optimize seeking without
// changing this validation contract.
func scanDurableCommits(dir string, knownKinds map[string]bool, visit func(Commit) bool) error {
	if knownKinds == nil {
		knownKinds = ProjectionKinds
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
	adapter := func(_ int64, commit Commit) bool {
		if visit == nil {
			return true
		}
		return visit(commit)
	}
	if manifest.Codec == Codec {
		return scanV4CommitFile(context.Background(), file, 0, 1, contentStoreForSessionDir(dir), knownKinds, adapter)
	}
	return scanCommitFileCodec(file, 0, 1, manifest.Codec, knownKinds, adapter)
}

func scanCommitFileCodec(file *os.File, startOffset int64, nextSequence uint64, codec string, knownKinds map[string]bool, visit func(int64, Commit) bool) error {
	if knownKinds == nil {
		knownKinds = ProjectionKinds
	}
	if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	next := nextSequence
	offset := startOffset
	operations := map[string]string{}
	for {
		recordOffset := offset
		line, readErr := reader.ReadBytes('\n')
		if errors.Is(readErr, io.EOF) {
			// Cold readers expose only the complete durable prefix. The exclusive
			// writer path preserves and repairs this tail before accepting work.
			break
		}
		if readErr != nil {
			return readErr
		}
		offset += int64(len(line))
		var commit Commit
		if err := json.Unmarshal(bytes.TrimSuffix(line, []byte{'\n'}), &commit); err != nil {
			return fmt.Errorf("%w: decode complete commit: %w", ErrDamagedStore, err)
		}
		if commit.SchemaVersion != 3 || commit.Codec != codec {
			return fmt.Errorf("%w: event codec", ErrUnsupportedVersion)
		}
		if commit.RecordType != "commit" || commit.ID == "" || commit.OperationID == "" ||
			commit.OperationHash == "" || commit.WriterGeneration == 0 || commit.FirstSequence != next ||
			commit.EventCount != len(commit.Events) || commit.EventCount == 0 {
			return fmt.Errorf("%w: invalid commit boundary at sequence %d", ErrDamagedStore, next)
		}
		if prior, ok := operations[commit.OperationID]; ok && prior != commit.OperationHash {
			return fmt.Errorf("%w: conflicting operation %q", ErrDamagedStore, commit.OperationID)
		}
		operations[commit.OperationID] = commit.OperationHash
		for i, event := range commit.Events {
			if event.Sequence != next+uint64(i) || event.ID == "" || strings.TrimSpace(event.Kind) == "" {
				return fmt.Errorf("%w: invalid event at sequence %d", ErrDamagedStore, next+uint64(i))
			}
			if !event.Optional && !knownKinds[event.Kind] {
				return fmt.Errorf("%w: unknown required event %q", ErrUnsupportedVersion, event.Kind)
			}
		}
		next = commit.LastSequence() + 1
		if visit != nil && !visit(recordOffset, commit) {
			return nil
		}
	}
	return nil
}

func preserveAndTruncateTail(path string, cut int64, label string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	info, err := input.Stat()
	if err != nil {
		_ = input.Close()
		return "", err
	}
	if cut < 0 || cut > info.Size() {
		_ = input.Close()
		return "", fmt.Errorf("invalid durable tail offset %d for %d-byte log", cut, info.Size())
	}
	backup := filepath.Join(filepath.Dir(path), fmt.Sprintf("events.%s-%d.tail", label, time.Now().UTC().UnixNano()))
	out, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = input.Close()
		return "", fmt.Errorf("preserve original tail: %w", err)
	}
	_, seekErr := input.Seek(cut, io.SeekStart)
	_, copyErr := io.CopyBuffer(out, input, make([]byte, 1<<20))
	syncErr := out.Sync()
	closeOutErr := out.Close()
	closeInErr := input.Close()
	if err := errors.Join(seekErr, copyErr, syncErr, closeOutErr, closeInErr); err != nil {
		_ = os.Remove(backup)
		return "", fmt.Errorf("preserve original tail: %w", err)
	}
	writable, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return "", err
	}
	truncateErr := writable.Truncate(cut)
	syncErr = writable.Sync()
	closeErr := writable.Close()
	if err := errors.Join(truncateErr, syncErr, closeErr); err != nil {
		return "", fmt.Errorf("truncate to durable prefix: %w", err)
	}
	return backup, nil
}

func hashOperation(sessionID, turnID string, events []Event) (string, error) {
	type inputEvent struct {
		ID       string          `json:"id,omitempty"`
		Kind     string          `json:"kind"`
		Optional bool            `json:"optional,omitempty"`
		Payload  json.RawMessage `json:"payload,omitempty"`
	}
	inputs := make([]inputEvent, len(events))
	for i, event := range events {
		inputs[i] = inputEvent{ID: event.ID, Kind: strings.TrimSpace(event.Kind), Optional: event.Optional, Payload: event.Payload}
	}
	b, err := json.Marshal(struct {
		SessionID string       `json:"sessionId"`
		TurnID    string       `json:"turnId"`
		Events    []inputEvent `json:"events"`
	}{SessionID: sessionID, TurnID: turnID, Events: inputs})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func deterministicID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:16])
}

func batchContains(events []Event, kind string) bool {
	for _, event := range events {
		if strings.TrimSpace(event.Kind) == kind {
			return true
		}
	}
	return false
}

func sameCommitPrefix(all, prefix []Commit) bool {
	for i := range prefix {
		if all[i].ID != prefix[i].ID {
			return false
		}
	}
	return true
}

func cloneEvents(events []Event) []Event {
	out := make([]Event, len(events))
	copy(out, events)
	for i := range out {
		out[i].Payload = append(json.RawMessage(nil), out[i].Payload...)
		if out[i].PayloadRef != nil {
			ref := *out[i].PayloadRef
			out[i].PayloadRef = &ref
		}
	}
	return out
}

func cloneCommit(commit Commit) Commit {
	commit.Events = cloneEvents(commit.Events)
	return commit
}

func cloneCommits(commits []Commit) []Commit {
	out := make([]Commit, len(commits))
	for i := range commits {
		out[i] = cloneCommit(commits[i])
	}
	return out
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
