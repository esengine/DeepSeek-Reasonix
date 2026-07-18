// Package history projects retained Session messages into the frozen Remote V1
// history contract and pages an immutable snapshot backwards by logical turn.
// It owns no Session lifecycle, filesystem path, transport, or contentRef data.
package history

import (
	"container/list"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
)

const (
	// DefaultSnapshotTTL matches the absolute lifetime of snapshot-owned
	// contentRef objects. It is an in-memory retention policy, not a wire
	// promise: eviction is always reported as SNAPSHOT_EXPIRED.
	DefaultSnapshotTTL = time.Hour
	// DefaultMaxSnapshots bounds retained immutable projections per daemon.
	DefaultMaxSnapshots = 256
	// DefaultSweepInterval reclaims expired projections even when no request is
	// currently touching the store.
	DefaultSweepInterval = time.Minute
)

var (
	ErrClosed             = errors.New("remote history store is closed")
	ErrInvalidCapture     = errors.New("invalid remote history capture")
	ErrInvalidPageTurns   = errors.New("history pageTurns must be between 1 and 200")
	ErrSnapshotExists     = errors.New("remote history snapshot already exists")
	ErrPageBudget         = errors.New("no complete history turn fits the final owner budget")
	ErrCursorIDExhausted  = errors.New("remote history cursor identity space exhausted")
	ErrCursorSecretFailed = errors.New("remote history cursor secret unavailable")
)

// Binding is the complete immutable provenance of one retained history
// projection. The Host epoch is included because the frozen cursor contract
// binds both Host and Session runtime incarnations.
type Binding struct {
	SnapshotID   protocol.SnapshotID
	HostEpoch    protocol.HostEpoch
	Target       protocol.RuntimeTarget
	RuntimeEpoch protocol.RuntimeEpoch
}

func (b Binding) validate() error {
	if strings.TrimSpace(string(b.SnapshotID)) == "" {
		return fmt.Errorf("%w: snapshotId is empty", ErrInvalidCapture)
	}
	if strings.TrimSpace(string(b.HostEpoch)) == "" {
		return fmt.Errorf("%w: hostEpoch is empty", ErrInvalidCapture)
	}
	if err := b.Target.Validate(); err != nil {
		return fmt.Errorf("%w: target: %v", ErrInvalidCapture, err)
	}
	if strings.TrimSpace(string(b.RuntimeEpoch)) == "" {
		return fmt.Errorf("%w: runtimeEpoch is empty", ErrInvalidCapture)
	}
	return nil
}

// MessageMetadata carries capture-time display state that provider.Message
// deliberately does not own. MessageIndex addresses Capture.Messages.
//
// DisplayContent and SubmitText distinguish absence from an intentional empty
// string. SubmitText is still sanitized by the projector so an internal Memory
// compiler contract can never become replayable UI text.
type MessageMetadata struct {
	MessageIndex   int
	DisplayContent *string
	SubmitText     *string
	CreatedAtMs    int64
}

// CheckpointBinding attaches an already-issued opaque checkpoint identity to
// the visible user message that owns it. This package never derives an ID from
// a turn number, timestamp, prompt, or path.
type CheckpointBinding struct {
	MessageIndex int
	CheckpointID protocol.CheckpointID
}

// SupplementalMessage inserts already-captured semantic display state after a
// provider message. AfterMessageIndex == -1 means the pre-turn prefix. It is
// used for retained phase/notice/compaction cards and planner display output
// that cannot be reconstructed from provider.Message alone. Supplemental user
// turns are intentionally rejected: logical-turn identity comes only from the
// canonical provider history.
type SupplementalMessage struct {
	AfterMessageIndex int
	Message           protocol.HistoryMessage
}

// Capture is a neutral, point-in-time Session history input. CaptureSnapshot
// deep-copies and projects every field before publishing the snapshot.
type Capture struct {
	Binding      Binding
	Messages     []provider.Message
	Metadata     []MessageMetadata
	Checkpoints  []CheckpointBinding
	Supplemental []SupplementalMessage
}

// Options controls daemon-local retention. Zero values select production
// defaults. Now is injectable for exact expiry tests. SweepInterval < 0 turns
// off the background sweeper; callers can invoke Sweep explicitly.
type Options struct {
	SnapshotTTL   time.Duration
	MaxSnapshots  int
	SweepInterval time.Duration
	Now           func() time.Time
}

// Stats is a point-in-time view after expired snapshots have been swept.
type Stats struct {
	Snapshots int
	Cursors   int
	Messages  int
	Closed    bool
}

type projectedEntry struct {
	turn    int
	message protocol.HistoryMessage
}

type snapshot struct {
	binding     Binding
	entries     []projectedEntry
	totalTurns  int
	expiresAt   time.Time
	cursorByEnd map[int]protocol.Cursor
	lru         *list.Element
}

type cursorRecord struct {
	snapshotID protocol.SnapshotID
	endTurn    int
}

// PageAcceptor performs the final owner-specific budget check for one raw
// candidate. It is called from the requested turn count downwards and must
// return accepted=false only for a size overflow that a smaller complete-turn
// page can resolve. The first accepted candidate is final; no later probe runs.
//
// This callback is the integration point for SessionSnapshot, whose 2 MiB
// budget includes state outside its nested HistoryPage. A caller can insert the
// candidate into the complete snapshot, externalize it, retain the successful
// result in its closure, and return true. Failed probes must not retain
// resources. The callback must not call back into this Store.
type PageAcceptor func(protocol.HistoryPage) (accepted bool, err error)

// Store is safe for concurrent capture, paging, release, sweep, and close.
// Snapshot projections are immutable after insertion; returned pages are deep
// copies so client-side externalization cannot mutate retained history.
type Store struct {
	mu sync.Mutex

	now          func() time.Time
	ttl          time.Duration
	maxSnapshots int
	secret       [32]byte
	cursorSeq    uint64

	snapshots map[protocol.SnapshotID]*snapshot
	cursors   map[protocol.Cursor]cursorRecord
	lru       list.List
	closed    bool

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New creates an epoch-local immutable history store.
func New(options Options) (*Store, error) {
	if options.SnapshotTTL < 0 {
		return nil, fmt.Errorf("history: negative snapshot TTL")
	}
	if options.MaxSnapshots < 0 {
		return nil, fmt.Errorf("history: negative snapshot capacity")
	}
	if options.SnapshotTTL == 0 {
		options.SnapshotTTL = DefaultSnapshotTTL
	}
	if options.MaxSnapshots == 0 {
		options.MaxSnapshots = DefaultMaxSnapshots
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	sweepInterval := options.SweepInterval
	if sweepInterval == 0 {
		sweepInterval = DefaultSweepInterval
		if halfTTL := options.SnapshotTTL / 2; halfTTL > 0 && halfTTL < sweepInterval {
			sweepInterval = halfTTL
		}
	}

	s := &Store{
		now: options.Now, ttl: options.SnapshotTTL, maxSnapshots: options.MaxSnapshots,
		snapshots: make(map[protocol.SnapshotID]*snapshot),
		cursors:   make(map[protocol.Cursor]cursorRecord),
		stop:      make(chan struct{}),
	}
	if _, err := rand.Read(s.secret[:]); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCursorSecretFailed, err)
	}
	if sweepInterval > 0 {
		s.wg.Add(1)
		go s.runSweeper(sweepInterval)
	}
	return s, nil
}

func (s *Store) runSweeper(interval time.Duration) {
	defer s.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Sweep()
		case <-s.stop:
			return
		}
	}
}

// CaptureSnapshot publishes one immutable history projection. Snapshot IDs
// must be unique for the daemon epoch; accidental replacement is rejected so a
// previously issued cursor can never change meaning.
func (s *Store) CaptureSnapshot(capture Capture) error {
	if err := capture.Binding.validate(); err != nil {
		return err
	}
	entries, totalTurns, err := projectCapture(capture)
	if err != nil {
		return err
	}

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.sweepExpiredLocked(now)
	if _, exists := s.snapshots[capture.Binding.SnapshotID]; exists {
		return ErrSnapshotExists
	}
	record := &snapshot{
		binding: capture.Binding, entries: entries, totalTurns: totalTurns,
		expiresAt: now.Add(s.ttl), cursorByEnd: make(map[int]protocol.Cursor),
	}
	record.lru = s.lru.PushFront(record)
	s.snapshots[capture.Binding.SnapshotID] = record
	for len(s.snapshots) > s.maxSnapshots {
		oldest, _ := s.lru.Back().Value.(*snapshot)
		s.removeSnapshotLocked(oldest)
	}
	return nil
}

// latest returns a full-body candidate for package tests and the budgeted
// wrappers. It is deliberately unexported: wire adapters must choose either
// LatestBudgeted or LatestFitting and cannot accidentally skip final budgeting.
func (s *Store) latest(binding Binding, pageTurns int) (protocol.HistoryPage, error) {
	return s.page(binding, "", pageTurns, true)
}

// older is the unexported full-body counterpart to latest. Unknown, expired,
// or context-mismatched history cursors all require a fresh subscribe and are
// reported as SNAPSHOT_EXPIRED, never STALE_CURSOR.
func (s *Store) older(binding Binding, cursor protocol.Cursor, pageTurns int) (protocol.HistoryPage, error) {
	if strings.TrimSpace(string(cursor)) == "" {
		return protocol.HistoryPage{}, fmt.Errorf("%w: cursor is empty", ErrInvalidCapture)
	}
	return s.page(binding, cursor, pageTurns, false)
}

func (s *Store) page(binding Binding, cursor protocol.Cursor, pageTurns int, latest bool) (protocol.HistoryPage, error) {
	if err := binding.validate(); err != nil {
		return protocol.HistoryPage{}, err
	}
	if pageTurns < 1 || pageTurns > protocol.HistoryMaxTurns {
		return protocol.HistoryPage{}, ErrInvalidPageTurns
	}

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return protocol.HistoryPage{}, ErrClosed
	}
	s.sweepExpiredLocked(now)
	record := s.snapshots[binding.SnapshotID]
	if record == nil || record.binding != binding {
		return protocol.HistoryPage{}, snapshotExpired(binding.Target)
	}
	endTurn := record.totalTurns
	if !latest {
		cursorBinding, ok := s.cursors[cursor]
		if !ok || cursorBinding.snapshotID != binding.SnapshotID {
			return protocol.HistoryPage{}, snapshotExpired(binding.Target)
		}
		endTurn = cursorBinding.endTurn
		if endTurn <= 0 || endTurn > record.totalTurns {
			return protocol.HistoryPage{}, snapshotExpired(binding.Target)
		}
	}
	s.lru.MoveToFront(record.lru)
	page, _, err := s.buildPageLocked(record, endTurn, pageTurns)
	return page, err
}

func (s *Store) buildPageLocked(record *snapshot, endTurn, pageTurns int) (protocol.HistoryPage, bool, error) {
	startTurn := endTurn - pageTurns
	if startTurn < 0 {
		startTurn = 0
	}
	page := protocol.HistoryPage{
		SnapshotID:   record.binding.SnapshotID,
		Messages:     make([]protocol.HistoryMessage, 0),
		StartTurn:    startTurn,
		EndTurn:      endTurn,
		TotalTurns:   record.totalTurns,
		ActualTurns:  endTurn - startTurn,
		HasOlder:     startTurn > 0,
		Externalized: []protocol.ExternalizedField{},
	}
	// Match the existing Desktop behavior for a history with no visible user
	// turn: a provider system prefix alone does not manufacture a page.
	if startTurn < endTurn {
		for _, entry := range record.entries {
			if entry.turn < 0 {
				if startTurn == 0 {
					page.Messages = append(page.Messages, cloneHistoryMessage(entry.message))
				}
				continue
			}
			if entry.turn >= startTurn && entry.turn < endTurn {
				page.Messages = append(page.Messages, cloneHistoryMessage(entry.message))
			}
		}
	}
	if page.HasOlder {
		cursor, created, err := s.cursorForBoundaryLocked(record, startTurn)
		if err != nil {
			return protocol.HistoryPage{}, false, err
		}
		page.NextCursor = cursor
		if err := page.Validate(); err != nil {
			if created {
				s.discardCursorLocked(record, startTurn, cursor)
			}
			return protocol.HistoryPage{}, false, fmt.Errorf("history: built invalid page: %w", err)
		}
		return page, created, nil
	}
	if err := page.Validate(); err != nil {
		return protocol.HistoryPage{}, false, fmt.Errorf("history: built invalid page: %w", err)
	}
	return page, false, nil
}

func (s *Store) cursorForBoundaryLocked(record *snapshot, endTurn int) (protocol.Cursor, bool, error) {
	if cursor := record.cursorByEnd[endTurn]; cursor != "" {
		return cursor, false, nil
	}
	for attempts := 0; attempts < 128; attempts++ {
		if s.cursorSeq == math.MaxUint64 {
			return "", false, ErrCursorIDExhausted
		}
		s.cursorSeq++
		var counter [8]byte
		binary.BigEndian.PutUint64(counter[:], s.cursorSeq)
		mac := hmac.New(sha256.New, s.secret[:])
		_, _ = mac.Write([]byte("reasonix.remote.history.cursor.v1\x00"))
		_, _ = mac.Write(counter[:])
		cursor := protocol.Cursor("hc_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
		if _, collision := s.cursors[cursor]; collision {
			continue
		}
		s.cursors[cursor] = cursorRecord{snapshotID: record.binding.SnapshotID, endTurn: endTurn}
		record.cursorByEnd[endTurn] = cursor
		return cursor, true, nil
	}
	return "", false, ErrCursorIDExhausted
}

func (s *Store) discardCursorLocked(record *snapshot, endTurn int, cursor protocol.Cursor) {
	if record.cursorByEnd[endTurn] != cursor {
		return
	}
	delete(record.cursorByEnd, endTurn)
	delete(s.cursors, cursor)
}

// LatestFitting selects the largest newest suffix, up to pageTurns, accepted by
// the final owner budget callback. It only reduces at complete logical-turn
// boundaries.
func (s *Store) LatestFitting(binding Binding, pageTurns int, accept PageAcceptor) (protocol.HistoryPage, error) {
	return s.fit(binding, "", pageTurns, true, accept)
}

// OlderFitting is LatestFitting for a snapshot-bound older-page cursor.
func (s *Store) OlderFitting(binding Binding, cursor protocol.Cursor, pageTurns int, accept PageAcceptor) (protocol.HistoryPage, error) {
	if strings.TrimSpace(string(cursor)) == "" {
		return protocol.HistoryPage{}, fmt.Errorf("%w: cursor is empty", ErrInvalidCapture)
	}
	return s.fit(binding, cursor, pageTurns, false, accept)
}

func (s *Store) fit(binding Binding, cursor protocol.Cursor, pageTurns int, latest bool, accept PageAcceptor) (protocol.HistoryPage, error) {
	if err := binding.validate(); err != nil {
		return protocol.HistoryPage{}, err
	}
	if pageTurns < 1 || pageTurns > protocol.HistoryMaxTurns {
		return protocol.HistoryPage{}, ErrInvalidPageTurns
	}
	if accept == nil {
		return protocol.HistoryPage{}, errors.New("history: nil page budget acceptor")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return protocol.HistoryPage{}, ErrClosed
	}
	s.sweepExpiredLocked(s.now())
	record := s.snapshots[binding.SnapshotID]
	if record == nil || record.binding != binding {
		return protocol.HistoryPage{}, snapshotExpired(binding.Target)
	}
	endTurn := record.totalTurns
	if !latest {
		cursorBinding, ok := s.cursors[cursor]
		if !ok || cursorBinding.snapshotID != binding.SnapshotID {
			return protocol.HistoryPage{}, snapshotExpired(binding.Target)
		}
		endTurn = cursorBinding.endTurn
		if endTurn <= 0 || endTurn > record.totalTurns {
			return protocol.HistoryPage{}, snapshotExpired(binding.Target)
		}
	}
	s.lru.MoveToFront(record.lru)

	maximum := pageTurns
	if maximum > endTurn {
		maximum = endTurn
	}
	if endTurn == 0 {
		maximum = 0
	}
	for turns := maximum; turns >= 0; turns-- {
		if turns == 0 && endTurn != 0 {
			break // never manufacture an empty page in place of one complete turn
		}
		page, cursorCreated, err := s.buildPageLocked(record, endTurn, turns)
		if err != nil {
			return protocol.HistoryPage{}, err
		}
		accepted, acceptErr := accept(page)
		if acceptErr != nil {
			if cursorCreated {
				s.discardCursorLocked(record, page.StartTurn, page.NextCursor)
			}
			return protocol.HistoryPage{}, acceptErr
		}
		if accepted {
			return page, nil
		}
		if cursorCreated {
			s.discardCursorLocked(record, page.StartTurn, page.NextCursor)
		}
	}
	return protocol.HistoryPage{}, ErrPageBudget
}

// Release explicitly invalidates one snapshot and all of its cursors. A
// mismatched binding does not reveal whether the snapshot ID exists.
func (s *Store) Release(binding Binding) bool {
	if binding.validate() != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	record := s.snapshots[binding.SnapshotID]
	if record == nil || record.binding != binding {
		return false
	}
	s.removeSnapshotLocked(record)
	return true
}

// Valid reports whether the complete binding still names a retained snapshot.
// It deliberately returns the same false result for malformed, unknown,
// provenance-mismatched, expired, evicted, and closed bindings so callers cannot
// use it to probe which part of a snapshot identity was correct. Validation does
// not extend the snapshot's absolute TTL or change capacity eviction order.
func (s *Store) Valid(binding Binding) bool {
	if s == nil || binding.validate() != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.sweepExpiredLocked(s.now())
	record := s.snapshots[binding.SnapshotID]
	return record != nil && record.binding == binding
}

// Sweep removes snapshots whose absolute retention TTL has elapsed.
func (s *Store) Sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0
	}
	before := len(s.snapshots)
	s.sweepExpiredLocked(s.now())
	return before - len(s.snapshots)
}

func (s *Store) sweepExpiredLocked(now time.Time) {
	for _, record := range s.snapshots {
		if !now.Before(record.expiresAt) {
			s.removeSnapshotLocked(record)
		}
	}
}

func (s *Store) removeSnapshotLocked(record *snapshot) {
	if record == nil || s.snapshots[record.binding.SnapshotID] != record {
		return
	}
	delete(s.snapshots, record.binding.SnapshotID)
	if record.lru != nil {
		s.lru.Remove(record.lru)
		record.lru = nil
	}
	for _, cursor := range record.cursorByEnd {
		delete(s.cursors, cursor)
	}
	for index := range record.entries {
		record.entries[index] = projectedEntry{}
	}
	record.entries = nil
	record.cursorByEnd = nil
}

// Stats sweeps expired records before reporting retained capacity.
func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.sweepExpiredLocked(s.now())
	}
	stats := Stats{Snapshots: len(s.snapshots), Cursors: len(s.cursors), Closed: s.closed}
	for _, record := range s.snapshots {
		stats.Messages += len(record.entries)
	}
	return stats
}

// Close drops every retained body and stops the background sweeper.
func (s *Store) Close() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for _, record := range s.snapshots {
		s.removeSnapshotLocked(record)
	}
	s.closed = true
}

func snapshotExpired(target protocol.RuntimeTarget) error {
	targetCopy := target
	return protocol.MustRemoteError(protocol.ErrSnapshotExpired, protocol.ErrorOptions{Target: &targetCopy})
}
