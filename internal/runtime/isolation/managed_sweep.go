package isolation

import (
	"context"
	"time"
)

// Sweep retries only already-committed cleanup, or expires prepared/ready
// candidates under the existing retention policy. It never publishes source
// changes or promotes an interrupted/unknown run to completed.
func (s *Store) sweepManaged(ctx context.Context, maxAge time.Duration) {
	for _, id := range s.managedIDs() {
		release, err := s.acquireResultLock(ctx, id, true)
		if err != nil {
			continue
		}
		s.sweepManagedLocked(ctx, id, maxAge)
		release()
	}
}

func (s *Store) sweepManagedLocked(ctx context.Context, id string, maxAge time.Duration) {
	r, found, err := s.managedRecord(id)
	if err != nil || !found {
		return
	}
	switch r.Phase {
	case candidateCleanup:
		_ = s.cleanupManaged(ctx, &r)
	case candidatePrepared, candidateReady:
		if r.Updated.IsZero() || time.Since(r.Updated) < maxAge {
			return
		}
		r.Phase, r.Disposition = candidateCleanup, "discarded"
		if s.saveManagedRecord(r, false) != nil {
			return
		}
		_ = s.cleanupManaged(ctx, &r)
	}
}
