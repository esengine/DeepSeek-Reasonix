package taskmonitor

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// defaultSchemaVersion is used when AppendEvent implicitly creates a
// TaskSnapshot for a task that has not been explicitly upserted.
const defaultSchemaVersion = 1

// InMemoryStore is a fully-in-memory Store implementation intended for
// testing and as a reference mock.  It is goroutine-safe.
//
// IMPORTANT: This store grows without bound (no eviction, no capacity
// limits).  It is NOT suitable for production use.  Production stores
// must implement resource caps and persistence.
type InMemoryStore struct {
	mu       sync.RWMutex
	tasks    map[string]*TaskSnapshot       // taskID → snapshot
	events   map[string][]TaskEvent         // taskID → ordered events
	byProj   map[string]map[string]struct{} // projectDir → set of taskIDs
	lastSeq  map[string]int                 // taskID → last seen sequence
	idemRecs map[string]*IdempotencyRecord  // key → record
}

// NewInMemoryStore returns a ready-to-use InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		tasks:   make(map[string]*TaskSnapshot),
		events:  make(map[string][]TaskEvent),
		byProj:  make(map[string]map[string]struct{}),
		lastSeq: make(map[string]int),
	}
}

// UpsertTask inserts or replaces a task snapshot and registers it under
// projectDir.  It is a test/convenience helper — not part of the Store
// interface.
func (s *InMemoryStore) UpsertTask(projectDir string, snap TaskSnapshot) error {
	if err := snap.Validate(); err != nil {
		return fmt.Errorf("upsert task: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := snap
	s.tasks[snap.TaskID] = &cp
	if s.byProj[projectDir] == nil {
		s.byProj[projectDir] = make(map[string]struct{})
	}
	s.byProj[projectDir][snap.TaskID] = struct{}{}
	return nil
}

// AppendEvent appends a validated event to the task's event log.
// It is a test/convenience helper — not part of the Store interface.
//
// Validation rules:
//   - Sequence must be strictly greater than the previous event's sequence
//     (monotonic increasing, no duplicates allowed).
//   - Events cannot be appended after the task has reached a terminal state.
//   - If the task already exists (from UpsertTask), the event's TaskID and
//     SessionID must match.
func (s *InMemoryStore) AppendEvent(projectDir string, ev TaskEvent) error {
	if err := ev.Validate(); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// sequence validation
	prev, hasPrev := s.lastSeq[ev.TaskID]
	if hasPrev {
		if ev.Sequence <= prev {
			return fmt.Errorf("append event: sequence %d is not strictly greater than previous %d",
				ev.Sequence, prev)
		}
	}

	// terminal-state guard
	if snap, ok := s.tasks[ev.TaskID]; ok && snap.State.Terminal() {
		return fmt.Errorf("append event: task %s is in terminal state %q",
			ev.TaskID, snap.State)
	}

	// identity validation
	if snap, ok := s.tasks[ev.TaskID]; ok {
		if ev.SessionID != snap.SessionID {
			return fmt.Errorf("append event: SessionID mismatch (event=%q, snapshot=%q)",
				ev.SessionID, snap.SessionID)
		}
	}

	// ensure task exists (at least minimally)
	if _, ok := s.tasks[ev.TaskID]; !ok {
		s.tasks[ev.TaskID] = &TaskSnapshot{
			SchemaVersion: defaultSchemaVersion,
			TaskID:        ev.TaskID,
			SessionID:     ev.SessionID,
			State:         ev.State,
			RuntimeState:  ev.RuntimeState,
			CreatedAt:     ev.Timestamp,
			UpdatedAt:     ev.Timestamp,
		}
	}
	s.lastSeq[ev.TaskID] = ev.Sequence

	if s.byProj[projectDir] == nil {
		s.byProj[projectDir] = make(map[string]struct{})
	}
	s.byProj[projectDir][ev.TaskID] = struct{}{}

	// Update snapshot from event.
	// ErrorCode and ErrorSummary are overwritten only when the event carries
	// a non-empty value; they are NOT cleared by events that lack them.
	snap := s.tasks[ev.TaskID]
	snap.State = ev.State
	if ev.RuntimeState != "" {
		snap.RuntimeState = ev.RuntimeState
	}
	snap.UpdatedAt = ev.Timestamp
	if ev.ErrorCode != "" {
		snap.ErrorCode = ev.ErrorCode
	}
	if ev.ErrorSummary != "" {
		snap.ErrorSummary = ev.ErrorSummary
	}

	s.events[ev.TaskID] = append(s.events[ev.TaskID], ev)
	return nil
}

// ListTasks implements Store.
func (s *InMemoryStore) ListTasks(ctx context.Context, projectDir string) ([]TaskSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ids []string
	if projectDir == "" {
		for id := range s.tasks {
			ids = append(ids, id)
		}
	} else {
		proj, ok := s.byProj[projectDir]
		if !ok {
			return []TaskSnapshot{}, nil
		}
		for id := range proj {
			ids = append(ids, id)
		}
	}

	result := make([]TaskSnapshot, 0, len(ids))
	for _, id := range ids {
		snap, ok := s.tasks[id]
		if !ok {
			continue
		}
		cp := *snap
		reconcileRuntime(&cp, timeNow())
		result = append(result, cp)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

// GetTask implements Store.
func (s *InMemoryStore) GetTask(ctx context.Context, projectDir string, taskID string) (*TaskSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// When projectDir is specified, verify the task belongs to that project.
	if projectDir != "" {
		proj, ok := s.byProj[projectDir]
		if !ok {
			return nil, nil
		}
		if _, ok := proj[taskID]; !ok {
			return nil, nil
		}
	}

	snap, ok := s.tasks[taskID]
	if !ok {
		return nil, nil
	}
	cp := *snap
	reconcileRuntime(&cp, timeNow())
	return &cp, nil
}

// ListEvents implements Store.
func (s *InMemoryStore) ListEvents(ctx context.Context, projectDir string, taskID string, afterSequence int) ([]TaskEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// When projectDir is specified, verify the task belongs to that project.
	if projectDir != "" {
		proj, ok := s.byProj[projectDir]
		if !ok {
			return []TaskEvent{}, nil
		}
		if _, ok := proj[taskID]; !ok {
			return []TaskEvent{}, nil
		}
	}

	all, ok := s.events[taskID]
	if !ok {
		return []TaskEvent{}, nil
	}
	result := make([]TaskEvent, 0)
	for _, e := range all {
		if e.Sequence > afterSequence {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Sequence < result[j].Sequence
	})
	return result, nil
}

// SaveTask implements WriteStore.
func (s *InMemoryStore) SaveTask(ctx context.Context, projectDir string, snap TaskSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.tasks[snap.TaskID]
	if !ok {
		return fmt.Errorf("save task: task %s not found", snap.TaskID)
	}
	// Version must be strictly greater (CAS check)
	if snap.Version <= existing.Version {
		return fmt.Errorf("save task: %w: stored=%d, given=%d", ErrStoreVersionConflict, existing.Version, snap.Version)
	}
	cp := snap
	s.tasks[snap.TaskID] = &cp
	if s.byProj[projectDir] == nil {
		s.byProj[projectDir] = make(map[string]struct{})
	}
	s.byProj[projectDir][snap.TaskID] = struct{}{}
	return nil
}

// RenewRuntimeLease implements WriteStore.
func (s *InMemoryStore) RenewRuntimeLease(ctx context.Context, projectDir, taskID, ownerID string, leaseUntil time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if ownerID == "" || leaseUntil.IsZero() {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if projectDir != "" {
		proj, ok := s.byProj[projectDir]
		if !ok {
			return false, nil
		}
		if _, ok := proj[taskID]; !ok {
			return false, nil
		}
	}
	snap, ok := s.tasks[taskID]
	if !ok || snap.RuntimeOwnerID != ownerID || snap.State.Terminal() || snap.RuntimeState.Effective() != RuntimeStateAlive {
		return false, nil
	}
	snap.Version++
	snap.RuntimeLeaseUntil = leaseUntil
	return true, nil
}

// AppendAuditEvent implements WriteStore.
func (s *InMemoryStore) AppendAuditEvent(ctx context.Context, projectDir string, ev TaskEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Atomically assign next sequence
	max := 0
	for _, e := range s.events[ev.TaskID] {
		if e.Sequence > max {
			max = e.Sequence
		}
	}
	ev.Sequence = max + 1
	if err := ev.Validate(); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	s.events[ev.TaskID] = append(s.events[ev.TaskID], ev)
	return nil
}

// CheckIdempotency implements WriteStore.
func (s *InMemoryStore) CheckIdempotency(ctx context.Context, projectDir string, key string) (*IdempotencyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_ = projectDir
	_ = ctx
	if s.idemRecs == nil {
		return nil, nil
	}
	rec, ok := s.idemRecs[key]
	if !ok {
		return nil, nil
	}
	cp := *rec
	return &cp, nil
}

// RecordIdempotency implements WriteStore.
func (s *InMemoryStore) RecordIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = projectDir
	_ = ctx
	if s.idemRecs == nil {
		s.idemRecs = make(map[string]*IdempotencyRecord)
	}
	// Reject if key already exists with different params
	if existing, ok := s.idemRecs[r.Key]; ok {
		if existing.Op != r.Op || existing.TaskID != r.TaskID || existing.Version != r.Version {
			return fmt.Errorf("idempotency key conflict: different params")
		}
		return nil // already recorded, idempotent
	}
	cp := r
	s.idemRecs[r.Key] = &cp
	return nil
}

func (s *InMemoryStore) ClaimIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) (*IdempotencyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = projectDir
	_ = ctx
	if s.idemRecs == nil {
		s.idemRecs = make(map[string]*IdempotencyRecord)
	}
	if r.ClaimedAt.IsZero() {
		r.ClaimedAt = timeNow()
	}
	r.Pending = true
	if existing, ok := s.idemRecs[r.Key]; ok {
		cp := *existing
		if cp.Pending && timeNow().Sub(cp.ClaimedAt) > 5*time.Minute {
			cp = r
			s.idemRecs[r.Key] = &cp
			return nil, nil
		}
		return &cp, nil
	}
	cp := r
	s.idemRecs[r.Key] = &cp
	return nil, nil
}

func (s *InMemoryStore) FinalizeIdempotency(ctx context.Context, projectDir string, r IdempotencyRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = projectDir
	_ = ctx
	existing, ok := s.idemRecs[r.Key]
	if !ok || existing.Op != r.Op || existing.TaskID != r.TaskID || existing.Version != r.Version {
		return fmt.Errorf("idempotency key conflict: different params")
	}
	existing.Pending = false
	return nil
}

func (s *InMemoryStore) ReleaseIdempotency(ctx context.Context, projectDir, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = projectDir
	_ = ctx
	if rec, ok := s.idemRecs[key]; ok && rec.Pending {
		delete(s.idemRecs, key)
	}
	return nil
}
