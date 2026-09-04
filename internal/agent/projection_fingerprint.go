package agent

import "reasonix/internal/provider"

// A projection stays valid only while the canonical prefix it folded is
// unchanged, and the check for that fingerprints every message behind the fold.
// Append-only growth cannot change that prefix; only a rewrite can.

// coveredHashMemo is one remembered fingerprint, valid for the rewrite counter
// it was taken under. Recomputing it per read cost 42ms and 83MB on a
// 40k-message transcript, on a path the status gauge alone walks each frame.
type coveredHashMemo struct {
	rewriteVersion int
	n              int
	hash           string
}

// prefixHasher returns a fingerprint function for messages captured under the
// given rewrite counter. Passing the counter in rather than reading it keeps
// the answer tied to the snapshot being checked: a rewrite that lands between
// the snapshot and the check must miss the memo, not silently validate it.
func (a *Agent) prefixHasher(rewriteVersion int) func([]provider.Message, int) string {
	return func(msgs []provider.Message, n int) string {
		if m := a.sess.coveredHash.Load(); m != nil && m.n == n && m.rewriteVersion == rewriteVersion {
			return m.hash
		}
		hash := coveredPrefixHash(msgs, n)
		a.sess.coveredHash.Store(&coveredHashMemo{rewriteVersion: rewriteVersion, n: n, hash: hash})
		return hash
	}
}

// visibleSnapshot is the canonical transcript plus everything a projection
// check needs to run against it without recomputing what has not changed.
type visibleSnapshot struct {
	msgs        []provider.Message
	version     uint64
	fingerprint func([]provider.Message, int) string
}

func (a *Agent) snapshotForProjection() visibleSnapshot {
	msgs, version, rewriteVersion := a.sess.conversation.snapshotWithVersion()
	return visibleSnapshot{msgs: msgs, version: version, fingerprint: a.prefixHasher(rewriteVersion)}
}

// visibleBehindMemoisedFold answers a turn from the projection plus the messages
// appended behind it, reading the tail rather than the canonical transcript. It
// runs only where the covered-prefix hash is already memoised: a fold or a
// rewrite moves the memo, and that one turn pays the full pass and refills it.
// The judgement is projectionCoversTail's, the same one the slow path makes.
func (a *Agent) visibleBehindMemoisedFold() ([]provider.Message, bool) {
	a.sess.compactionMu.Lock()
	st := a.sess.compactionState
	key := a.currentPromptCacheKeyLocked()
	a.sess.compactionMu.Unlock()

	covered := st.Projection.CoveredCount
	if len(st.Projection.Messages) == 0 || covered <= 0 || !projectionLineageOK(st, key) {
		return nil, false
	}
	tail, total, _, rewriteVersion := a.sess.conversation.snapshotTail(covered)
	memo := a.sess.coveredHash.Load()
	if memo == nil || memo.n != covered || memo.rewriteVersion != rewriteVersion {
		return nil, false
	}
	if !projectionCoversTail(st, total, memo.hash) {
		return nil, false
	}
	out := make([]provider.Message, 0, len(st.Projection.Messages)+len(tail))
	out = append(out, st.Projection.Messages...)
	out = append(out, tail...)
	return out, len(out) > 0
}
