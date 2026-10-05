package workspacelease

import "time"

// Stats is one session's whole account of the workspace write lease, closed
// when the lease is released. It decides nothing: no gate reads it and no
// prompt byte moves with it. It exists because the question "does serialising
// writers actually cost anyone anything" had no number behind it — the only
// record was a sentence on screen, and only for a wait that outlived the grace.
type Stats struct {
	// Contended counts acquisitions that did not succeed on their first try,
	// including the ones that cleared inside the grace and were never reported.
	// Those are the common case and the one a notice cannot see.
	Contended int
	// Reported counts the subset that outlived the grace and became a notice.
	Reported int
	// Waited is the total time spent waiting across Contended acquisitions.
	Waited time.Duration
	// Held is from the acquisition to the release.
	Held time.Duration
	// Idle is the part of Held after the last call to AcquireWrite — held while
	// writing nothing. Every mutation asks, so the last ask dates the last
	// write attempt, and this is what an earlier release would give back.
	Idle time.Duration
}

// StatsNotice receives one closed account. It must return quickly and must not
// call back into Owner.
type StatsNotice func(Stats)

// OnRelease sets the sink for the account closed at each release. It is not a
// constructor argument because a lease is usable without one, and every test
// that builds an Owner would otherwise have to say so.
func (o *Owner) OnRelease(notice StatsNotice) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.onRelease = notice
	o.mu.Unlock()
}

// askedLocked records that a writer asked for the lease. Called on every
// AcquireWrite, held or not, because what it dates is the last write attempt.
func (o *Owner) askedLocked(at time.Time) {
	o.lastAsk = at
}

// contendedLocked records one acquisition that had to wait.
func (o *Owner) contendedLocked(waited time.Duration, reported bool) {
	o.stats.Contended++
	o.stats.Waited += waited
	if reported {
		o.stats.Reported++
	}
}

// closeStatsLocked returns the account for the acquisition now ending and
// resets it, so a session that takes the lease twice reports twice.
func (o *Owner) closeStatsLocked(now time.Time) (Stats, bool) {
	if o.onRelease == nil || o.acquiredAt.IsZero() {
		o.stats, o.acquiredAt, o.lastAsk = Stats{}, time.Time{}, time.Time{}
		return Stats{}, false
	}
	out := o.stats
	out.Held = now.Sub(o.acquiredAt)
	// The ask that took the lease is dated before the acquisition it waited
	// for, so the stretch runs from whichever came later. Idle is part of the
	// hold and can never be longer than it.
	since := o.acquiredAt
	if o.lastAsk.After(since) {
		since = o.lastAsk
	}
	if now.After(since) {
		out.Idle = now.Sub(since)
	}
	o.stats, o.acquiredAt, o.lastAsk = Stats{}, time.Time{}, time.Time{}
	return out, true
}
