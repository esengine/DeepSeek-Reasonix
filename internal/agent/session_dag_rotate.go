package agent

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// sessionDAGWriterQuietPeriod is how long another writer must have been silent
// before this process may rewrite the log under it.
const sessionDAGWriterQuietPeriod = 60 * time.Second

// SessionRotationDeniedError reports why a log could not be rotated or
// upgraded right now; the log simply keeps growing until the condition clears.
type SessionRotationDeniedError struct {
	Reason string
}

func (e *SessionRotationDeniedError) Error() string {
	return "session log rotation denied: " + e.Reason
}

// sessionDAGSingleWriterProof is the precondition for any rewrite of a schema-2
// log: this runtime holds the lease, no handoff is reserved, and no other
// writer has appended within the quiet period. The caller holds the file lock.
func sessionDAGSingleWriterProof(sessionPath string, st *sessionDAGState, now time.Time) error {
	if !SessionLeaseHeldByCurrentRuntime(sessionPath) {
		return &SessionRotationDeniedError{Reason: "session lease is not held by this runtime"}
	}
	if info, err := LoadSessionLeaseInfo(sessionPath); err == nil && handoffReservationActive(info, now) {
		return &SessionRotationDeniedError{Reason: "lease handoff reservation is active"}
	}
	me := SessionWriterID()
	for id, w := range st.writers {
		if id == me || id == "" {
			continue
		}
		if now.Sub(w.lastActivity) < sessionDAGWriterQuietPeriod {
			return &SessionRotationDeniedError{Reason: fmt.Sprintf("writer %s appended %s ago", id, now.Sub(w.lastActivity).Round(time.Second))}
		}
	}
	return nil
}

// sessionDAGLogOversized bounds a schema-2 log at the schema-1 growth factor
// over the encoded size of every live head's chain.
func sessionDAGLogOversized(st *sessionDAGState) bool {
	live := int64(0)
	for _, id := range st.liveHeads() {
		msgs, _ := st.materialize(id)
		if _, size, err := digestAndSizeSessionMessages(msgs); err == nil {
			live += size
		}
	}
	return sessionEventLogOversized(st.size, live)
}

// rotateSessionDAG atomically replaces the log with the next generation: live
// heads and their chains keep their ids, patches are folded, redactions are
// applied physically, and everything unreachable is dropped. The caller must
// hold the file lock and have obtained the single-writer proof.
func rotateSessionDAG(sessionPath string, st *sessionDAGState, now time.Time) error {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return fmt.Errorf("empty session event log path")
	}
	entries, err := buildRotatedSessionDAG(st, now)
	if err != nil {
		return err
	}
	data, err := encodeSessionDAGEntries(entries, now)
	if err != nil {
		return err
	}
	fileutil.Crash("dag-rotate", path)
	staged, err := fileutil.StageAtomicWrite(path, data, 0o600)
	if err != nil {
		return err
	}
	// The marker brackets the window an unlocked appender cannot see into:
	// bytes it lands before the late read are carried, bytes after it are
	// re-appended by the appender once the marker clears.
	marker := store.SessionEventLogRotating(sessionPath)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		_ = os.Remove(staged)
		return err
	}
	defer os.Remove(marker)
	if hook := sessionDAGRotateBeforeReplace; hook != nil {
		hook(sessionPath)
	}
	if err := appendLateLinesToStaged(path, st.size, staged); err != nil {
		_ = os.Remove(staged)
		return err
	}
	return fileutil.PublishStagedWrite(staged, path)
}

func buildRotatedSessionDAG(st *sessionDAGState, now time.Time) ([]sessionDAGEntry, error) {
	keep := st.reachable()
	live := st.liveHeads()
	selected := st.selectedHead()

	var dropped, tombstones []string
	for id := range st.nodes {
		if _, ok := keep[id]; !ok {
			dropped = append(dropped, id)
		}
	}
	for id := range st.redactions {
		if _, ok := keep[id]; ok {
			tombstones = append(tombstones, id)
		}
	}
	slices.Sort(dropped)
	slices.Sort(tombstones)

	heads := make([]SessionHead, 0, len(live))
	for _, id := range live {
		heads = append(heads, st.headRecord(id, selected))
	}
	entries := []sessionDAGEntry{
		{Type: sessionDAGTypeLog, At: now, Generation: st.generation + 1, RotatedFrom: st.generation, UpgradedFrom: st.upgradedFrom},
		{Type: sessionDAGTypeCheckpoint, At: now, SelectedHead: selected, Heads: heads, Dropped: dropped, Tombstones: tombstones},
	}

	writers := make([]*sessionDAGWriter, 0, len(st.writers))
	for _, w := range st.writers {
		writers = append(writers, w)
	}
	slices.SortFunc(writers, func(a, b *sessionDAGWriter) int { return cmp.Compare(a.id, b.id) })
	for _, w := range writers {
		entries = append(entries, sessionDAGEntry{Type: sessionDAGTypeWriter, Writer: w.id, At: w.lastActivity, PID: w.pid, Hostname: w.hostname, LeaseGeneration: w.leaseGeneration})
	}

	forks := make([]*sessionDAGHead, 0, len(live))
	for _, id := range live {
		if id != SessionMainHead {
			forks = append(forks, st.heads[id])
		}
	}
	slices.SortStableFunc(forks, func(a, b *sessionDAGHead) int {
		if c := a.createdAt.Compare(b.createdAt); c != 0 {
			return c
		}
		return cmp.Compare(a.id, b.id)
	})
	for _, h := range forks {
		entries = append(entries, sessionDAGEntry{Type: sessionDAGTypeFork, Head: h.parentHead, NewHead: h.id, From: h.forkFrom, Kind: h.kind, Name: h.name, Writer: h.writer, At: h.createdAt})
	}
	if main := st.heads[SessionMainHead]; main != nil && main.name != "" && !main.retired {
		entries = append(entries, sessionDAGEntry{Type: sessionDAGTypeRename, Head: SessionMainHead, Name: main.name, At: main.createdAt})
	}
	for _, id := range live {
		h := st.heads[id]
		if h.system == nil {
			continue
		}
		raw, err := encodeSessionDAGMessage(*h.system)
		if err != nil {
			return nil, err
		}
		entries = append(entries, sessionDAGEntry{Type: sessionDAGTypeSystem, Head: id, Msgs: raw, At: h.createdAt})
	}

	nodes := make([]*sessionDAGNode, 0, len(keep))
	for id := range keep {
		nodes = append(nodes, st.nodes[id])
	}
	slices.SortFunc(nodes, func(a, b *sessionDAGNode) int { return cmp.Compare(a.offset, b.offset) })
	digests := map[string]string{}
	for _, n := range nodes {
		m := st.appliedMessage(n)
		parentDigest := ""
		if _, ok := keep[n.parent]; ok {
			parentDigest = digests[n.parent]
		}
		e, err := newSessionDAGMessageEntry(n.head, n.parent, parentDigest, n.turn, m, n.at)
		if err != nil {
			return nil, err
		}
		if _, ok := keep[n.parent]; !ok {
			e.Parent = ""
		}
		e.Writer = n.writer
		digests[n.id] = e.Digest
		entries = append(entries, e)
	}

	for _, id := range live {
		h := st.heads[id]
		if c := h.compaction; c != nil {
			if _, ok := keep[c.coveredLeaf]; ok {
				entries = append(entries, sessionDAGEntry{Type: sessionDAGTypeCompaction, Head: id, CoveredLeaf: c.coveredLeaf, CoveredCount: c.coveredCount, PrefixHash: c.prefixHash, At: c.at})
			}
		}
		if t := h.openTurn; t != nil {
			entries = append(entries, sessionDAGEntry{Type: sessionDAGTypeTurnBegin, Head: id, Turn: t.turn, Leaf: t.leaf, PreserveUser: t.preserveUser, Writer: t.writer, At: t.at})
		}
	}
	if h := st.heads[st.selected]; h != nil && !h.retired {
		entries = append(entries, sessionDAGEntry{Type: sessionDAGTypeSelect, Head: st.selected, Reason: "rotation", At: now})
	}
	return entries, nil
}

// upgradeSessionLogToDAG replaces a schema-1 log with generation 1 of the
// schema-2 log: one message entry per transcript message under the main head,
// keeping the ids the caller already assigned, plus an open turn marker when
// the schema-1 in-flight sidecar recorded one. The caller holds the file lock
// and has obtained the single-writer proof.
func upgradeSessionLogToDAG(sessionPath string, msgs []provider.Message, times []time.Time, inFlight *InFlightTurnMeta, now time.Time) error {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return fmt.Errorf("empty session event log path")
	}
	host, _ := os.Hostname()
	entries := []sessionDAGEntry{
		{Type: sessionDAGTypeLog, At: now, Generation: 1, UpgradedFrom: sessionEventSchemaVersion},
		{Type: sessionDAGTypeWriter, At: now, PID: os.Getpid(), Hostname: host},
	}
	parent, digest := "", ""
	for i, m := range msgs {
		if m.ID == "" {
			return fmt.Errorf("upgrade session log: message %d has no id", i)
		}
		at := now
		if i < len(times) && !times[i].IsZero() {
			at = times[i]
		}
		e, err := newSessionDAGMessageEntry(SessionMainHead, parent, digest, "", m, at)
		if err != nil {
			return err
		}
		entries = append(entries, e)
		parent, digest = m.ID, e.Digest
	}
	if inFlight != nil && inFlight.ID != "" && inFlight.StartMessageIndex >= 0 && inFlight.StartMessageIndex <= len(msgs) {
		leaf := ""
		if inFlight.StartMessageIndex > 0 {
			leaf = msgs[inFlight.StartMessageIndex-1].ID
		}
		at := inFlight.StartedAt
		if at.IsZero() {
			at = now
		}
		entries = append(entries, sessionDAGEntry{Type: sessionDAGTypeTurnBegin, Head: SessionMainHead, Turn: inFlight.ID, Leaf: leaf, PreserveUser: inFlight.PreserveUser, At: at})
	}
	data, err := encodeSessionDAGEntries(entries, now)
	if err != nil {
		return err
	}
	fileutil.Crash("dag-upgrade", path)
	return fileutil.AtomicWriteFileStrict(path, data, 0o600)
}
