package sessioninbox

import "time"

// RequeueUnappliedSteer returns a steer accepted into a turn that ended before
// consuming it to the queue as an ordinary follow-up, keeping its place in the
// FIFO. It reports whether it moved the item: an item in any other state is
// left alone, so a repeat is a no-op and nothing is delivered twice.
func (s *Store) RequeueUnappliedSteer(id string) (bool, error) {
	if s == nil {
		return false, ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.beginDiskTransactionLocked()
	if err != nil {
		return false, err
	}
	defer release()
	if err := s.mutableLocked(); err != nil {
		return false, err
	}
	next := s.man.clone()
	i := next.indexOf(id)
	if i < 0 {
		return false, ErrNotFound
	}
	if next.Items[i].State != StateSteerAccepted {
		return false, nil
	}
	next.Items[i].State = StateQueued
	next.Items[i].Intent = IntentFollowup
	next.Items[i].BlockReason, next.Items[i].BlockCode = "", ""
	next.Items[i].UpdatedAt = time.Now().UTC()
	if err := s.commitManifestLocked(next); err != nil {
		return false, err
	}
	s.notifyLocked(s.snapshotLocked())
	return true, nil
}
