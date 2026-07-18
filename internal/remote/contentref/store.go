// Package contentref owns daemon-epoch-scoped, in-memory storage for Remote V1
// session semantic content. It deliberately has no filesystem, path, Git, or
// transfer API.
package contentref

import (
	"container/list"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/remote/protocol"
)

const (
	// DefaultMaxBytes bounds retained content across the daemon epoch. Entries
	// are evicted least-recently-used before this limit is exceeded.
	DefaultMaxBytes int64 = 128 << 20
	// DefaultMaxEntries bounds metadata independently of content bytes.
	DefaultMaxEntries = 4096

	contentRefRandomBytes = 32
	contentRefPrefix      = "cr_"
	contentRefTokenBytes  = 43 // RawURLEncoding length for 32 random bytes.
	maxIDAttempts         = 128
	maxInjectedIssuedIDs  = 1 << 20
)

var (
	ErrClosed        = errors.New("contentref: daemon epoch store is closed")
	ErrCapacity      = errors.New("contentref: owner content exceeds store capacity")
	ErrInvalidOffset = errors.New("contentref: byte offset exceeds content length")
	ErrInvalidUTF8   = errors.New("contentref: externalized content must be valid UTF-8")
	ErrInvalidOwner  = errors.New("contentref: invalid owner identity")
	ErrInvalidLease  = errors.New("contentref: invalid lease identity")
	ErrEpochMismatch = errors.New("contentref: owner belongs to a different daemon epoch")
	ErrIDExhausted   = errors.New("contentref: could not issue a unique content reference")
)

// Config supplies deterministic dependencies for tests and memory bounds for
// an epoch store. Zero limits select the production defaults.
type Config struct {
	Now func() time.Time
	// newID is package-private so production callers cannot replace the
	// cryptographic issuer. Tests use it to exercise collision handling.
	newID      func() (protocol.ContentRef, error)
	MaxBytes   int64
	MaxEntries int
}

// Stats is a point-in-time view of live retained entries. Issued counts all IDs
// ever signed during this Store's lifetime, including released and evicted IDs.
type Stats struct {
	Entries int
	Owners  int
	Bytes   int64
	Issued  int
	Closed  bool
}

// ReferenceKind identifies the only three frozen content owners. Snapshot and
// history share the snapshot kind because both are invalidated by snapshotId.
type ReferenceKind string

const (
	ReferenceSnapshot ReferenceKind = "snapshot"
	ReferenceEvent    ReferenceKind = "event"
)

// ReferenceBinding is daemon-internal provenance returned atomically with a
// chunk. It is not serialized on the wire: session/content still accepts only
// opaque contentRef + offset.
type ReferenceBinding struct {
	Kind         ReferenceKind
	HostEpoch    protocol.HostEpoch
	LeaseID      protocol.LeaseID
	Target       protocol.RuntimeTarget
	SnapshotID   protocol.SnapshotID
	RuntimeEpoch protocol.RuntimeEpoch
	Seq          uint64
}

type ownerKind uint8

const (
	ownerSnapshot ownerKind = iota + 1
	ownerEvent
)

// Owner is the non-forgeable internal binding carried by every contentRef.
// Snapshot and history owners share snapshotId identity. Event owners use the
// runtimeEpoch + seq pair required by the frozen protocol.
type Owner struct {
	kind         ownerKind
	snapshotID   protocol.SnapshotID
	runtimeEpoch protocol.RuntimeEpoch
	seq          uint64
}

// SnapshotOwner returns the shared owner identity for a SessionSnapshot and
// any HistoryPage produced from that snapshot.
func SnapshotOwner(snapshotID protocol.SnapshotID) (Owner, error) {
	if strings.TrimSpace(string(snapshotID)) == "" {
		return Owner{}, ErrInvalidOwner
	}
	return Owner{kind: ownerSnapshot, snapshotID: snapshotID}, nil
}

// EventOwner returns the owner identity for one complete SessionEvent envelope.
func EventOwner(runtimeEpoch protocol.RuntimeEpoch, seq uint64) (Owner, error) {
	if strings.TrimSpace(string(runtimeEpoch)) == "" || seq == 0 {
		return Owner{}, ErrInvalidOwner
	}
	return Owner{kind: ownerEvent, runtimeEpoch: runtimeEpoch, seq: seq}, nil
}

func (o Owner) key() (string, error) {
	switch o.kind {
	case ownerSnapshot:
		if strings.TrimSpace(string(o.snapshotID)) == "" {
			return "", ErrInvalidOwner
		}
		return "snapshot\x00" + string(o.snapshotID), nil
	case ownerEvent:
		if strings.TrimSpace(string(o.runtimeEpoch)) == "" || o.seq == 0 {
			return "", ErrInvalidOwner
		}
		return fmt.Sprintf("event\x00%s\x00%d", o.runtimeEpoch, o.seq), nil
	default:
		return "", ErrInvalidOwner
	}
}

type entry struct {
	ref        protocol.ContentRef
	ownerKey   string
	leaseID    protocol.LeaseID
	binding    ReferenceBinding
	data       []byte
	sha256     string
	createdAt  time.Time
	lastAccess time.Time
	lru        *list.Element
}

type storedField struct {
	pointer        string
	data           []byte
	originalBytes  int64
	truncated      bool
	truncationNote string
}

// Store holds content for exactly one daemon HostEpoch. Close invalidates every
// reference and permanently prevents further issuance from this Store.
type Store struct {
	mu sync.Mutex

	hostEpoch protocol.HostEpoch
	now       func() time.Time
	newID     func() (protocol.ContentRef, error)
	idSecret  [32]byte
	idCounter uint64
	issuedN   int
	maxBytes  int64
	maxRefs   int

	closed  bool
	bytes   int64
	entries map[protocol.ContentRef]*entry
	owners  map[string]map[protocol.ContentRef]struct{}
	// issued is only used by the bounded injected-generator test path. The
	// production counter+HMAC issuer proves uniqueness without lifetime-growing
	// metadata.
	issued map[protocol.ContentRef]struct{}
	lru    list.List // front is most recently used; back is eviction candidate.
}

func New(hostEpoch protocol.HostEpoch, config Config) (*Store, error) {
	if strings.TrimSpace(string(hostEpoch)) == "" {
		return nil, ErrInvalidOwner
	}
	if config.MaxBytes < 0 || config.MaxEntries < 0 {
		return nil, ErrCapacity
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = DefaultMaxBytes
	}
	if config.MaxEntries == 0 {
		config.MaxEntries = DefaultMaxEntries
	}
	store := &Store{
		hostEpoch: hostEpoch,
		now:       config.Now,
		newID:     config.newID,
		maxBytes:  config.MaxBytes,
		maxRefs:   config.MaxEntries,
		entries:   make(map[protocol.ContentRef]*entry),
		owners:    make(map[string]map[protocol.ContentRef]struct{}),
	}
	if config.newID != nil {
		store.issued = make(map[protocol.ContentRef]struct{})
	} else if _, err := rand.Read(store.idSecret[:]); err != nil {
		return nil, fmt.Errorf("contentref: initialize opaque ID issuer: %w", err)
	}
	return store, nil
}

func validGeneratedRef(ref protocol.ContentRef) bool {
	raw := string(ref)
	if len(raw) != len(contentRefPrefix)+contentRefTokenBytes || !strings.HasPrefix(raw, contentRefPrefix) {
		return false
	}
	for _, char := range raw[len(contentRefPrefix):] {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func (s *Store) HostEpoch() protocol.HostEpoch {
	if s == nil {
		return ""
	}
	return s.hostEpoch
}

// Read implements the frozen session/content payload: the caller supplies only
// an opaque contentRef and raw byte offset. Unknown, released, evicted, expired,
// and closed references are intentionally indistinguishable.
func (s *Store) Read(params protocol.SessionContentParams) (protocol.SessionContentResult, error) {
	result, _, err := s.read(Owner{}, false, "", false, params)
	return result, err
}

// ReadForLease is the daemon-facing session/content lookup. It preserves
// content across a transport-generation reconnect that resumes the same lease,
// while making references issued to an expired or replaced lease opaque.
func (s *Store) ReadForLease(leaseID protocol.LeaseID, params protocol.SessionContentParams) (protocol.SessionContentResult, error) {
	result, _, err := s.ReadBoundForLease(leaseID, params)
	return result, err
}

// ReadBoundForLease atomically returns a chunk and its internal owner
// provenance. The daemon uses this to reject a ref after its snapshot or
// runtime incarnation has ceased to be current.
func (s *Store) ReadBoundForLease(
	leaseID protocol.LeaseID,
	params protocol.SessionContentParams,
) (protocol.SessionContentResult, ReferenceBinding, error) {
	if strings.TrimSpace(string(leaseID)) == "" {
		return protocol.SessionContentResult{}, ReferenceBinding{}, expiredError()
	}
	return s.read(Owner{}, false, leaseID, true, params)
}

// ReadForOwner additionally verifies the reference's snapshotId or
// runtimeEpoch+seq binding. It is useful at owner lifecycle boundaries; a
// mismatch is exposed as CONTENT_REF_EXPIRED so identity is not leaked.
func (s *Store) ReadForOwner(owner Owner, params protocol.SessionContentParams) (protocol.SessionContentResult, error) {
	result, _, err := s.read(owner, true, "", false, params)
	return result, err
}

func (s *Store) read(
	owner Owner,
	checkOwner bool,
	leaseID protocol.LeaseID,
	checkLease bool,
	params protocol.SessionContentParams,
) (protocol.SessionContentResult, ReferenceBinding, error) {
	if s == nil {
		return protocol.SessionContentResult{}, ReferenceBinding{}, expiredError()
	}
	var ownerKey string
	var err error
	if checkOwner {
		ownerKey, err = owner.key()
		if err != nil {
			return protocol.SessionContentResult{}, ReferenceBinding{}, expiredError()
		}
	}
	if params.Offset < 0 {
		return protocol.SessionContentResult{}, ReferenceBinding{}, ErrInvalidOffset
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return protocol.SessionContentResult{}, ReferenceBinding{}, expiredError()
	}
	now := s.now()
	s.purgeExpiredLocked(now)
	item, ok := s.entries[params.ContentRef]
	if !ok || (checkOwner && item.ownerKey != ownerKey) || (checkLease && item.leaseID != leaseID) {
		return protocol.SessionContentResult{}, ReferenceBinding{}, expiredError()
	}
	if params.Offset > int64(len(item.data)) {
		return protocol.SessionContentResult{}, ReferenceBinding{}, ErrInvalidOffset
	}
	start := int(params.Offset)
	end := start + protocol.ContentRefChunkBytes
	if end > len(item.data) {
		end = len(item.data)
	}
	chunk := item.data[start:end]
	if now.After(item.lastAccess) {
		item.lastAccess = now
	}
	s.lru.MoveToFront(item.lru)

	result := protocol.SessionContentResult{
		ContentRef: params.ContentRef,
		Offset:     params.Offset,
		DataBase64: base64.StdEncoding.EncodeToString(chunk),
		TotalBytes: int64(len(item.data)),
		SHA256:     item.sha256,
		Encoding:   protocol.ContentUTF8,
	}
	if end < len(item.data) {
		next := int64(end)
		result.NextOffset = &next
	}
	return result, item.binding, nil
}

// Release invalidates one reference. Its opaque identity remains reserved for
// the rest of the Store lifetime and can never be issued again.
func (s *Store) Release(ref protocol.ContentRef) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	item, ok := s.entries[ref]
	if !ok {
		return false
	}
	s.removeLocked(item)
	return true
}

// ReleaseOwner invalidates all content bound to one snapshotId or event
// runtimeEpoch+seq identity.
func (s *Store) ReleaseOwner(owner Owner) int {
	if s == nil {
		return 0
	}
	ownerKey, err := owner.key()
	if err != nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0
	}
	refs := s.owners[ownerKey]
	count := len(refs)
	for ref := range refs {
		if item := s.entries[ref]; item != nil {
			s.removeLocked(item)
		}
	}
	return count
}

// Close ends the daemon epoch and invalidates every live contentRef.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.bytes = 0
	s.entries = make(map[protocol.ContentRef]*entry)
	s.owners = make(map[string]map[protocol.ContentRef]struct{})
	s.lru.Init()
}

func (s *Store) Stats() Stats {
	if s == nil {
		return Stats{Closed: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.purgeExpiredLocked(s.now())
	}
	return Stats{Entries: len(s.entries), Owners: len(s.owners), Bytes: s.bytes, Issued: s.issuedN, Closed: s.closed}
}

func (s *Store) putBatch(
	owner Owner,
	leaseID protocol.LeaseID,
	binding ReferenceBinding,
	fields []storedField,
) ([]protocol.ExternalizedField, error) {
	ownerKey, err := owner.key()
	if err != nil {
		return nil, err
	}
	if leaseID != "" && strings.TrimSpace(string(leaseID)) == "" {
		return nil, ErrInvalidLease
	}
	binding.HostEpoch = s.hostEpoch
	binding.LeaseID = leaseID
	switch owner.kind {
	case ownerSnapshot:
		binding.Kind = ReferenceSnapshot
		binding.SnapshotID = owner.snapshotID
	case ownerEvent:
		binding.Kind = ReferenceEvent
		binding.RuntimeEpoch = owner.runtimeEpoch
		binding.Seq = owner.seq
	default:
		return nil, ErrInvalidOwner
	}
	if leaseID != "" && (strings.TrimSpace(string(binding.Target.WorkspaceID)) == "" ||
		strings.TrimSpace(string(binding.Target.SessionID)) == "" ||
		strings.TrimSpace(string(binding.RuntimeEpoch)) == "") {
		return nil, ErrInvalidOwner
	}
	if len(fields) == 0 {
		return nil, nil
	}
	var batchBytes int64
	for _, field := range fields {
		if !utf8.Valid(field.data) {
			return nil, ErrInvalidUTF8
		}
		batchBytes += int64(len(field.data))
	}
	if batchBytes > s.maxBytes || len(fields) > s.maxRefs {
		return nil, ErrCapacity
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	now := s.now()
	s.purgeExpiredLocked(now)

	refs := make([]protocol.ContentRef, len(fields))
	for i := range fields {
		ref, err := s.issueLocked(ownerKey)
		if err != nil {
			return nil, err
		}
		refs[i] = ref
	}
	for s.bytes+batchBytes > s.maxBytes || len(s.entries)+len(fields) > s.maxRefs {
		candidate := s.lru.Back()
		if candidate == nil {
			return nil, ErrCapacity
		}
		s.removeLocked(candidate.Value.(*entry))
	}

	descriptors := make([]protocol.ExternalizedField, len(fields))
	for i, field := range fields {
		data := append([]byte(nil), field.data...)
		digest := sha256.Sum256(data)
		sha := hex.EncodeToString(digest[:])
		item := &entry{
			ref: refs[i], ownerKey: ownerKey, leaseID: leaseID, binding: binding, data: data, sha256: sha,
			createdAt: now, lastAccess: now,
		}
		item.lru = s.lru.PushFront(item)
		s.entries[item.ref] = item
		if s.owners[ownerKey] == nil {
			s.owners[ownerKey] = make(map[protocol.ContentRef]struct{})
		}
		s.owners[ownerKey][item.ref] = struct{}{}
		s.bytes += int64(len(data))

		descriptor := protocol.ExternalizedField{
			JSONPointer: field.pointer,
			ContentRef:  item.ref,
			TotalBytes:  int64(len(data)),
			SHA256:      sha,
			Truncated:   field.truncated,
		}
		if field.truncated {
			original := field.originalBytes
			descriptor.OriginalBytes = &original
			descriptor.TruncationReason = field.truncationNote
		}
		descriptors[i] = descriptor
	}
	return descriptors, nil
}

func (s *Store) issueLocked(ownerKey string) (protocol.ContentRef, error) {
	if s.newID == nil {
		if s.idCounter == ^uint64(0) {
			return "", ErrIDExhausted
		}
		s.idCounter++
		var counter [8]byte
		binary.BigEndian.PutUint64(counter[:], s.idCounter)
		mac := hmac.New(sha256.New, s.idSecret[:])
		_, _ = mac.Write([]byte(s.hostEpoch))
		_, _ = mac.Write([]byte{0})
		_, _ = mac.Write([]byte(ownerKey))
		_, _ = mac.Write([]byte{0})
		_, _ = mac.Write(counter[:])
		tag := mac.Sum(nil)
		raw := make([]byte, 0, contentRefRandomBytes)
		raw = append(raw, counter[:]...)
		raw = append(raw, tag[:contentRefRandomBytes-len(counter)]...)
		s.issuedN++
		return protocol.ContentRef(contentRefPrefix + base64.RawURLEncoding.EncodeToString(raw)), nil
	}
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		ref, err := s.newID()
		if err != nil {
			return "", err
		}
		if !validGeneratedRef(ref) {
			return "", fmt.Errorf("%w: generator returned a malformed opaque ID", ErrIDExhausted)
		}
		if _, exists := s.issued[ref]; exists {
			continue
		}
		if len(s.issued) >= maxInjectedIssuedIDs {
			return "", ErrIDExhausted
		}
		s.issued[ref] = struct{}{}
		s.issuedN++
		return ref, nil
	}
	return "", ErrIDExhausted
}

func (s *Store) purgeExpiredLocked(now time.Time) {
	idle := time.Duration(protocol.ContentRefIdleMillis) * time.Millisecond
	maxAge := time.Duration(protocol.ContentRefMaxAgeMillis) * time.Millisecond
	for _, item := range s.entries {
		if (!now.Before(item.createdAt.Add(maxAge))) || (!now.Before(item.lastAccess.Add(idle))) {
			s.removeLocked(item)
		}
	}
}

func (s *Store) removeLocked(item *entry) {
	if item == nil || s.entries[item.ref] != item {
		return
	}
	delete(s.entries, item.ref)
	s.bytes -= int64(len(item.data))
	s.lru.Remove(item.lru)
	if refs := s.owners[item.ownerKey]; refs != nil {
		delete(refs, item.ref)
		if len(refs) == 0 {
			delete(s.owners, item.ownerKey)
		}
	}
}

func expiredError() error {
	return protocol.MustRemoteError(protocol.ErrContentRefExpired, protocol.ErrorOptions{})
}
