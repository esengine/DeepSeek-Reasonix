// A background job's evidence is merged into the turn that owns it, and the
// lease is what records that it was, so the same job is not counted twice.
package evidence

// BackgroundLease identifies a background job whose evidence was provisionally
// merged into the current turn's ledger. The host commits these leases only
// after the turn passes its delivery gates, so a failed turn leaves the job's
// evidence collectable again.
type BackgroundLease struct {
	Session string
	JobID   string
}

// ResetBackgroundLeases starts a new run inside the same delivery scope. The
// durable receipts remain available, while per-run job leases must be collected
// and committed independently.
func (l *Ledger) ResetBackgroundLeases() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.backgroundLeases = nil
	l.mu.Unlock()
}

// NoteBackgroundLease records that a background job's evidence was merged into
// this turn. It returns false when the job was already noted this turn so the
// caller can skip a duplicate merge — collection is idempotent within a turn,
// while a fresh turn (after Reset) leases again.
func (l *Ledger) NoteBackgroundLease(session, jobID string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, lease := range l.backgroundLeases {
		if lease.Session == session && lease.JobID == jobID {
			return false
		}
	}
	l.backgroundLeases = append(l.backgroundLeases, BackgroundLease{Session: session, JobID: jobID})
	return true
}

// BackgroundLeases returns the background jobs merged into this turn, for the
// host to commit once the turn's delivery gates pass.
func (l *Ledger) BackgroundLeases() []BackgroundLease {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.backgroundLeases) == 0 {
		return nil
	}
	out := make([]BackgroundLease, len(l.backgroundLeases))
	copy(out, l.backgroundLeases)
	return out
}
