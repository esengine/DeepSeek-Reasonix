package sessioninbox

import (
	"strings"
	"time"
)

// DeleteItem removes metadata first, then the blob (crash may leave orphan).
func (s *Store) DeleteItem(id string) error {
	if s == nil {
		return ErrClosed
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	meta, ok := s.man.item(id)
	if !ok {
		return ErrNotFound
	}
	if !isPendingState(meta.State) {
		return ErrInvalidState
	}
	next := s.man.clone()
	keys := next.idempotencyKeysFor(id)
	next.rememberReceipt(keys, id, Disposition("deleted"), time.Now().UTC())
	removed, _ := next.removeItem(id)
	clearPauseIfEmpty(next)
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.removeBlobLocked(blobNameFor(removed))
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// DiscardPendingItems removes the named, not-yet-admitted items in one
// manifest transaction. Missing IDs are treated as already consumed so a
// frontend may safely cancel from a slightly stale metadata snapshot. Items
// that have crossed the admission boundary are never deleted here.
func (s *Store) DiscardPendingItems(ids []string) error {
	return s.DiscardPendingItemsOwned(ids, "")
}

// DiscardPendingItemsOwned atomically removes pending IDs belonging to source.
// Foreign-source IDs are ignored so one frontend cannot cancel another.
func (s *Store) DiscardPendingItemsOwned(ids []string, source string) error {
	if s == nil {
		return ErrClosed
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	for _, item := range s.man.Items {
		if _, ok := wanted[item.ID]; !ok {
			continue
		}
		if source != "" && item.Source != source {
			continue
		}
		switch item.State {
		case StateQueued, StateBlocked, StateUncertain:
		default:
			return ErrInvalidState
		}
	}

	next := s.man.clone()
	removed := make([]InboxItemMeta, 0, len(wanted))
	kept := next.Items[:0]
	for _, item := range next.Items {
		_, selected := wanted[item.ID]
		owned := source == "" || item.Source == source
		if selected && owned {
			removed = append(removed, item)
			continue
		}
		kept = append(kept, item)
	}
	if len(removed) == 0 {
		return nil
	}
	next.Items = kept
	now := time.Now().UTC()
	for _, item := range removed {
		keys := next.idempotencyKeysFor(item.ID)
		next.rememberReceipt(keys, item.ID, Disposition("discarded"), now)
		for _, key := range keys {
			delete(next.Idempotency, key)
			delete(next.IdempotencyHashes, key)
		}
	}
	clearPauseIfEmpty(next)
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	for _, item := range removed {
		s.removeBlobLocked(blobNameFor(item))
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// MoveItem reorders the queue. toIndex is 0-based; values past the end append.
func (s *Store) MoveItem(id string, toIndex int) error {
	if s == nil {
		return ErrClosed
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	next := s.man.clone()
	from := next.indexOf(id)
	if from < 0 {
		return ErrNotFound
	}
	if !isPendingState(next.Items[from].State) {
		return ErrInvalidState
	}
	if toIndex < 0 {
		toIndex = 0
	}
	if toIndex >= len(next.Items) {
		toIndex = len(next.Items) - 1
	}
	if from == toIndex {
		return nil
	}
	it := next.Items[from]
	next.Items = append(next.Items[:from], next.Items[from+1:]...)
	if toIndex > len(next.Items) {
		toIndex = len(next.Items)
	}
	next.Items = append(next.Items[:toIndex], append([]InboxItemMeta{it}, next.Items[toIndex:]...)...)
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// SetPaused toggles the recovery/inspection pause flag.
func (s *Store) SetPaused(paused bool) error {
	if s == nil {
		return ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	if s.man.Paused == paused {
		return nil
	}
	next := s.man.clone()
	next.Paused = paused
	if !paused {
		next.Recovered = false
		next.RecoveredN = 0
	}
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// PauseIfPending pauses dispatch only when the inbox still contains work.
func (s *Store) PauseIfPending() error {
	if s == nil {
		return ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	if len(s.man.Items) == 0 || s.man.Paused {
		return nil
	}
	next := s.man.clone()
	next.Paused = true
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// SetState transitions one item's durable state.
func (s *Store) SetState(id string, state InboxState, blockReason string) error {
	if s == nil {
		return ErrClosed
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	next := s.man.clone()
	i := next.indexOf(id)
	if i < 0 {
		return ErrNotFound
	}
	next.Items[i].State = state
	next.Items[i].BlockReason = blockReason
	next.Items[i].UpdatedAt = time.Now().UTC()
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// ClaimItem atomically transitions one queued item to running. It is the
// durable admission boundary for both asynchronous and synchronous frontends.
func (s *Store) ClaimItem(id string) error {
	if s == nil {
		return ErrClosed
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	if s.man.Paused {
		return ErrPaused
	}
	next := s.man.clone()
	i := next.indexOf(id)
	if i < 0 {
		return ErrNotFound
	}
	if next.Items[i].State != StateQueued {
		return ErrInvalidState
	}
	next.Items[i].State = StateRunning
	next.Items[i].BlockReason = ""
	next.Items[i].UpdatedAt = time.Now().UTC()
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// ConvertIntent changes followup ↔ steer while keeping the item queued.
func (s *Store) ConvertIntent(id string, intent InboxIntent) error {
	if s == nil {
		return ErrClosed
	}
	if intent != IntentSteer {
		intent = IntentFollowup
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	next := s.man.clone()
	i := next.indexOf(id)
	if i < 0 {
		return ErrNotFound
	}
	if !isPendingState(next.Items[i].State) {
		return ErrInvalidState
	}
	next.Items[i].Intent = intent
	next.Items[i].UpdatedAt = time.Now().UTC()
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// AckDequeue removes a running/consumed item after durable transcript commit.
func (s *Store) AckDequeue(id string) error {
	if s == nil {
		return ErrClosed
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	next := s.man.clone()
	keys := next.idempotencyKeysFor(id)
	next.rememberReceipt(keys, id, Disposition("acknowledged"), time.Now().UTC())
	removed, ok := next.removeItem(id)
	if !ok {
		return ErrNotFound
	}
	switch removed.State {
	case StateRunning, StateSteerAccepted, StateSteerConsumed:
	default:
		return ErrInvalidState
	}
	clearPauseIfEmpty(next)
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.removeBlobLocked(blobNameFor(removed))
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// RetryItem resets uncertain/blocked items to queued.
func (s *Store) RetryItem(id string) error {
	if s == nil {
		return ErrClosed
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return err
	}
	next := s.man.clone()
	i := next.indexOf(id)
	if i < 0 {
		return ErrNotFound
	}
	switch next.Items[i].State {
	case StateUncertain, StateBlocked:
		next.Items[i].State = StateQueued
		next.Items[i].BlockReason = ""
		next.Items[i].UpdatedAt = time.Now().UTC()
	default:
		return ErrInvalidState
	}
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

// NextQueued returns the first FIFO queued follow-up (or rejected steer kept as
// follow-up) when the inbox is not paused.
func (s *Store) NextQueued() (InboxItemMeta, bool) {
	if s == nil {
		return InboxItemMeta{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if release, err := s.beginDiskTransactionLocked(); err == nil {
		release()
	}
	if s.man == nil || s.man.Paused || s.readonly {
		return InboxItemMeta{}, false
	}
	for _, it := range s.man.Items {
		if it.State == StateQueued && it.Intent == IntentFollowup {
			return it, true
		}
		// Rejected steers that remain intent=steer but queued are still follow-ups
		// for the dispatcher after ConvertIntent; only followup intent is admitted.
	}
	// Also admit steer-intent items that are still queued (user wants them as turns).
	for _, it := range s.man.Items {
		if it.State == StateQueued {
			return it, true
		}
	}
	return InboxItemMeta{}, false
}

// Pause marks paused=true without requiring a mutation check beyond schema.
func (s *Store) ForcePause(reasonRecovered bool, n int) error {
	if s == nil {
		return ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return err
	}
	defer release()
	if s.closed || s.readonly {
		if s.readonly {
			return nil
		}
		return ErrClosed
	}
	next := s.man.clone()
	next.Paused = true
	if reasonRecovered {
		next.Recovered = true
		if n > 0 {
			next.RecoveredN = n
		}
	}
	if err := s.commitManifestLocked(next); err != nil {
		return err
	}
	s.notifyLocked(s.snapshotLocked())
	return nil
}

func clearPauseIfEmpty(m *manifest) {
	if m == nil || len(m.Items) > 0 {
		return
	}
	m.Paused = false
	m.Recovered = false
	m.RecoveredN = 0
}

func isPendingState(state InboxState) bool {
	switch state {
	case StateQueued, StateBlocked, StateUncertain:
		return true
	default:
		return false
	}
}
