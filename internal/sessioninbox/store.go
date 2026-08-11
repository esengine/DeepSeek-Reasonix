package sessioninbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/filelock"
	"reasonix/internal/store"
)

const (
	manifestName     = "manifest.json"
	blobsDirName     = "blobs"
	quarantineName   = "quarantine"
	blobSuffix       = ".json"
	diskLockName     = "transaction.lock"
	diskLockWait     = 5 * time.Second
	maxManifestBytes = 8 << 20
)

// Store is the transactional durable inbox for one session path.
// Disk I/O runs under store.mu only; callers must not hold Controller locks.
type Store struct {
	mu       sync.Mutex
	dir      string
	session  string // session transcript path
	runID    string
	limits   Limits
	man      *manifest
	readonly bool
	closed   bool
	// listeners receive revision bumps after durable commits (non-blocking).
	listeners []func(InboxSnapshot)
}

// Open binds a Store to the session's inbox directory. The directory is created
// for its transaction lock; body blobs remain lazy. Cross-process recovery
// marks uncertain items and pauses.
func Open(sessionPath string, limits Limits) (*Store, error) {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return nil, fmt.Errorf("sessioninbox: empty session path")
	}
	dir := store.SessionInboxDir(sessionPath)
	s := &Store{
		dir:     dir,
		session: sessionPath,
		runID:   ProcessRunID(),
		limits:  limits.withDefaults(),
		man:     emptyManifest(ProcessRunID()),
	}
	if err := s.loadOrInit(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the on-disk inbox directory.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dir
}

// SessionPath returns the bound session transcript path.
func (s *Store) SessionPath() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session
}

// Rebind moves the store to a new session path without copying future work
// (used after rename migration that already relocated the directory).
func (s *Store) Rebind(sessionPath string) error {
	if s == nil {
		return ErrClosed
	}
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return fmt.Errorf("sessioninbox: empty session path")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.session = sessionPath
	s.dir = store.SessionInboxDir(sessionPath)
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	release()
	return nil
}

// Close seals the store. Further mutations fail with ErrClosed.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// OnChange registers a non-blocking snapshot listener.
func (s *Store) OnChange(fn func(InboxSnapshot)) {
	if s == nil || fn == nil {
		return
	}
	s.mu.Lock()
	s.listeners = append(s.listeners, fn)
	s.mu.Unlock()
}

func (s *Store) loadOrInit() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	release()
	return nil
}

// beginDiskTransactionLocked serializes every manifest read/modify/write with
// other Store instances and processes, then refreshes the in-memory snapshot.
// The caller must hold s.mu and call the returned release function.
func (s *Store) beginDiskTransactionLocked() (func(), error) {
	if err := ensurePrivateDir(s.dir); err != nil {
		return nil, fmt.Errorf("sessioninbox: create inbox directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), diskLockWait)
	defer cancel()
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, diskLockName))
	if err != nil {
		return nil, fmt.Errorf("sessioninbox: acquire disk lock: %w", err)
	}
	if err := s.loadOrInitLocked(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (s *Store) loadOrInitLocked() error {
	path := filepath.Join(s.dir, manifestName)
	data, err := readRegularFile(path, maxManifestBytes)
	if errors.Is(err, os.ErrNotExist) {
		s.man = emptyManifest(s.runID)
		s.readonly = false
		return nil
	}
	if err != nil {
		return fmt.Errorf("sessioninbox: read manifest: %w", err)
	}
	man, err := decodeManifest(data)
	if err != nil {
		// Corrupt manifest → quarantine, salvage orphan blobs as uncertain
		// items, pause for user inspection. Never present "0 recovered".
		_ = s.quarantineFileLocked(path, "manifest-corrupt")
		salvaged := s.salvageOrphanBlobsLocked()
		s.man = emptyManifest(s.runID)
		s.man.Paused = true
		s.man.Recovered = true
		s.man.RecoveredN = len(salvaged)
		s.man.Items = salvaged
		return s.commitManifestLocked(s.man)
	}
	if man.SchemaVersion > SchemaVersion {
		s.man = man
		s.readonly = true
		s.man.Paused = true
		return nil
	}
	migrated := man.SchemaVersion < SchemaVersion
	if migrated {
		for key, id := range man.Idempotency {
			if man.IdempotencyHashes[key] != "" {
				continue
			}
			meta, ok := man.item(id)
			if !ok {
				return fmt.Errorf("sessioninbox: migrate idempotency target: %w", ErrNotFound)
			}
			env, err := s.readBlobLocked(blobNameFor(meta), meta.Checksum)
			if err != nil {
				return fmt.Errorf("sessioninbox: migrate idempotency body: %w", err)
			}
			hash, err := idempotencyRequestHash(env)
			if err != nil {
				return fmt.Errorf("sessioninbox: migrate idempotency hash: %w", err)
			}
			man.IdempotencyHashes[key] = hash
		}
		man.SchemaVersion = SchemaVersion
	}
	// Cross-process recovery: another run left in-flight items.
	recovered := 0
	if man.RunID != "" && man.RunID != s.runID {
		for i := range man.Items {
			switch man.Items[i].State {
			case StateRunning, StateSteerAccepted, StateSteerConsumed:
				man.Items[i].State = StateUncertain
				man.Items[i].UpdatedAt = time.Now().UTC()
				recovered++
			case StateQueued, StateBlocked, StateUncertain:
				recovered++
			}
		}
		if recovered > 0 || len(man.Items) > 0 {
			man.Paused = true
			man.Recovered = true
			man.RecoveredN = recovered
		}
	}
	man.RunID = s.runID
	s.man = man
	s.readonly = false
	if recovered > 0 || migrated {
		return s.commitManifestLocked(man)
	}
	// GC orphan blobs without holding callers longer than needed.
	s.gcOrphansLocked()
	return nil
}

// Snapshot returns a copy of current metadata.
func (s *Store) Snapshot() InboxSnapshot {
	if s == nil {
		return InboxSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if release, err := s.beginDiskTransactionLocked(); err == nil {
		release()
	}
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() InboxSnapshot {
	m := s.man
	if m == nil {
		m = emptyManifest(s.runID)
	}
	items := append([]InboxItemMeta(nil), m.Items...)
	return InboxSnapshot{
		SchemaVersion: m.SchemaVersion,
		Revision:      m.Revision,
		Paused:        m.Paused,
		Recovered:     m.Recovered,
		RecoveredN:    m.RecoveredN,
		Readonly:      s.readonly,
		RunID:         m.RunID,
		Items:         items,
		Capacity: Capacity{
			Items:        len(items),
			MaxItems:     s.limits.MaxItems,
			Bytes:        m.totalBytes(),
			MaxBytes:     s.limits.MaxTotalBytes,
			MaxItemBytes: s.limits.MaxItemBytes,
		},
	}
}

// Enqueue durably appends an item. Only returns a receipt after blob+manifest
// commit succeed. Idempotent keys return the original item.
func (s *Store) Enqueue(req EnqueueRequest) (InboxReceipt, error) {
	if s == nil {
		return InboxReceipt{}, ErrClosed
	}
	env := completeEnqueueEnvelope(req.Envelope)
	hasInvocation := env.Invocation != nil || len(env.Invocations) > 0
	if strings.TrimSpace(env.SubmitText) == "" && strings.TrimSpace(env.DisplayText) == "" && strings.TrimSpace(env.RawText) == "" && !hasInvocation {
		return InboxReceipt{}, ErrEmpty
	}
	intent := req.Intent
	if intent != IntentSteer {
		intent = IntentFollowup
	}
	idem := strings.TrimSpace(firstNonEmpty(req.Idempotency, env.Idempotency))
	if idem != "" && !validIdempotencyKey(idem) {
		return InboxReceipt{}, fmt.Errorf("sessioninbox: invalid idempotency key")
	}
	source := strings.TrimSpace(firstNonEmpty(req.Source, env.Source))

	blobBytes, checksum, byteSize, err := encodeEnvelope(env)
	if err != nil {
		return InboxReceipt{}, err
	}
	requestHash, err := idempotencyRequestHash(env)
	if err != nil {
		return InboxReceipt{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return InboxReceipt{}, err
	}
	defer release()
	if s.closed {
		return InboxReceipt{}, ErrClosed
	}
	if s.readonly {
		return InboxReceipt{}, ErrSchemaReadonly
	}
	if receipt, found, err := s.idempotentReceiptLocked(idem, requestHash); err != nil || found {
		return receipt, err
	}
	if byteSize > s.limits.MaxItemBytes {
		return InboxReceipt{}, ErrItemTooLarge
	}
	if len(s.man.Items) >= s.limits.MaxItems {
		return InboxReceipt{}, ErrCapacityItems
	}
	if s.man.totalBytes()+byteSize > s.limits.MaxTotalBytes {
		return InboxReceipt{}, ErrCapacityBytes
	}

	id := newRandomID()
	blobName := id
	now := time.Now().UTC()
	meta := InboxItemMeta{
		ID:          id,
		SessionID:   firstNonEmpty(req.SessionID, agentBranchID(s.session)),
		Intent:      intent,
		State:       StateQueued,
		Revision:    s.man.Revision + 1,
		BlobName:    blobName,
		Source:      source,
		CreatedAt:   now,
		UpdatedAt:   now,
		Preview:     PreviewText(env.DisplayText, DefaultPreviewRunes),
		ByteSize:    byteSize,
		Checksum:    checksum,
		Idempotency: idem,
		Refs:        refSummaries(env.Refs),
		RunID:       s.runID,
	}

	// Transaction: write blob → commit manifest → receipt.
	if err := s.writeBlobLocked(blobName, blobBytes); err != nil {
		return InboxReceipt{}, err
	}
	next := s.man.clone()
	next.Items = append(next.Items, meta)
	bindIdempotency(next, idem, id, requestHash)
	if err := s.commitManifestLocked(next); err != nil {
		s.removeBlobLocked(blobName)
		return InboxReceipt{}, err
	}
	snap := s.snapshotLocked()
	s.notifyLocked(snap)
	return InboxReceipt{
		ItemID:      id,
		Disposition: DispositionQueuedFollowup,
		Position:    len(next.Items),
		Paused:      next.Paused,
		Capacity:    snap.Capacity,
	}, nil
}

// ReadItem loads a full PromptEnvelope by ID.
func (s *Store) ReadItem(id string) (InboxItemMeta, PromptEnvelope, error) {
	if s == nil {
		return InboxItemMeta{}, PromptEnvelope{}, ErrClosed
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return InboxItemMeta{}, PromptEnvelope{}, err
	}
	defer release()
	if s.closed {
		return InboxItemMeta{}, PromptEnvelope{}, ErrClosed
	}
	meta, ok := s.man.item(id)
	if !ok {
		return InboxItemMeta{}, PromptEnvelope{}, ErrNotFound
	}
	env, err := s.readBlobLocked(blobNameFor(meta), meta.Checksum)
	if err != nil {
		return meta, PromptEnvelope{}, err
	}
	return meta, env, nil
}

// UpdateItem writes a new immutable blob, switches the manifest pointer, then
// deletes the old blob. Pre-commit crashes leave only a GC-able orphan; after
// commit the checksum always points at the new body.
func (s *Store) UpdateItem(id string, env PromptEnvelope) (InboxItemMeta, error) {
	return s.UpdateItemWithIdempotency(id, env, "", PromptEnvelope{})
}

// UpdateItemWithIdempotency atomically updates an item and optionally binds an
// additional client idempotency key to it. aliasEnvelope is the original client
// request, not the merged body, so collect-mode redelivery remains deduplicated.
func (s *Store) UpdateItemWithIdempotency(id string, env PromptEnvelope, alias string, aliasEnvelope PromptEnvelope) (InboxItemMeta, error) {
	if s == nil {
		return InboxItemMeta{}, ErrClosed
	}
	id = strings.TrimSpace(id)
	alias = strings.TrimSpace(alias)
	if alias != "" && !validIdempotencyKey(alias) {
		return InboxItemMeta{}, fmt.Errorf("sessioninbox: invalid idempotency key")
	}
	env = normalizeEnvelope(env)
	if strings.TrimSpace(env.SubmitText) == "" && env.Invocation == nil && len(env.Invocations) == 0 {
		return InboxItemMeta{}, ErrEmpty
	}
	blobBytes, checksum, byteSize, err := encodeEnvelope(env)
	if err != nil {
		return InboxItemMeta{}, err
	}
	aliasHash := ""
	if alias != "" {
		aliasEnvelope = completeEnqueueEnvelope(aliasEnvelope)
		aliasHash, err = idempotencyRequestHash(aliasEnvelope)
		if err != nil {
			return InboxItemMeta{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return InboxItemMeta{}, err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return InboxItemMeta{}, err
	}
	meta, ok := s.man.item(id)
	if !ok {
		return InboxItemMeta{}, ErrNotFound
	}
	if !isPendingState(meta.State) {
		return InboxItemMeta{}, ErrInvalidState
	}
	replayed, err := s.idempotentAliasReplayLocked(alias, aliasHash, id)
	if err != nil {
		return InboxItemMeta{}, err
	}
	if replayed {
		return meta, nil
	}
	if byteSize > s.limits.MaxItemBytes {
		return InboxItemMeta{}, ErrItemTooLarge
	}
	delta := byteSize - meta.ByteSize
	if s.man.totalBytes()+delta > s.limits.MaxTotalBytes {
		return InboxItemMeta{}, ErrCapacityBytes
	}
	oldBlob := blobNameFor(meta)
	newBlob := id + "." + newRandomID()
	if err := s.writeBlobLocked(newBlob, blobBytes); err != nil {
		return InboxItemMeta{}, err
	}
	next := s.man.clone()
	i := next.indexOf(id)
	next.Items[i].BlobName = newBlob
	next.Items[i].ByteSize = byteSize
	next.Items[i].Checksum = checksum
	next.Items[i].Preview = PreviewText(env.DisplayText, DefaultPreviewRunes)
	next.Items[i].Refs = refSummaries(env.Refs)
	next.Items[i].UpdatedAt = time.Now().UTC()
	next.Items[i].Revision = next.Revision + 1
	bindIdempotency(next, alias, id, aliasHash)
	if next.Items[i].State == StateBlocked {
		next.Items[i].State = StateQueued
		next.Items[i].BlockReason = ""
	}
	if err := s.commitManifestLocked(next); err != nil {
		s.removeBlobLocked(newBlob)
		return InboxItemMeta{}, err
	}
	if oldBlob != newBlob {
		s.removeBlobLocked(oldBlob)
	}
	updated := next.Items[i]
	s.notifyLocked(s.snapshotLocked())
	return updated, nil
}

func (s *Store) removeBlobLocked(blobName string) {
	if err := validatePrivateDir(filepath.Join(s.dir, blobsDirName)); err != nil {
		return
	}
	path, err := s.blobPath(blobName)
	if err == nil {
		_ = os.Remove(path)
	}
}
