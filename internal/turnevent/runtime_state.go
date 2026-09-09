package turnevent

import "reasonix/internal/event"

// RuntimeIdentity returns the last committed lifecycle identity and watermark
// under one ledger lock. It performs no replay, compaction or file reads.
func (l *Ledger) RuntimeIdentity() (string, event.TurnStatus, uint64) {
	if l == nil {
		return "", "", 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active, l.status, l.latestLocked()
}
