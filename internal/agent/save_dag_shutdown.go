package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// AppendForShutdownWithoutLock persists the unsaved tail of a schema-2
// session after the cross-process save lock stayed held for the whole
// bounded wait at shutdown. The entries are appended without that lock, on
// a fresh concurrent head so no locked writer can be extending the same
// chain, and the derived cache files are left to the next locked save.
// handled is false for a schema-1 log, whose shutdown path is still a
// recovery copy.
func (s *Session) AppendForShutdownWithoutLock(path string, rewrite bool) (handled bool, err error) {
	if strings.TrimSpace(path) == "" {
		return false, fmt.Errorf("empty session path")
	}
	unlock := lockSessionSavePath(path)
	defer unlock()
	probe, err := probeSessionEventLog(path)
	if err != nil || !probe.dag {
		return false, err
	}
	mode := sessionSaveSnapshot
	if rewrite {
		mode = sessionSaveRewrite
	}
	return true, s.appendUnlockedLocked(path, mode)
}

func (s *Session) appendUnlockedLocked(path string, mode sessionSaveMode) error {
	ctx := context.Background()
	now := time.Now().UTC()
	msgs, version, rewriteVersion := s.snapshotWithVersion()
	digest, _, err := digestAndSizeSessionMessages(msgs)
	if err != nil {
		return err
	}
	st, err := s.dagStateWithoutLock(ctx, path)
	if err != nil {
		return err
	}
	s.ensureMessageIDsForSave(msgs)
	plan, err := s.planDAGWrite(path, st, msgs, mode, now)
	if err != nil {
		var conflict *SessionSnapshotConflictError
		if errors.As(err, &conflict) {
			// Disk is ahead and this session added nothing: the next open reads it.
			return nil
		}
		return err
	}
	pending := s.takePendingMarkers()
	entries := shutdownHeadBatch(plan, pending, now)
	baseRevision, _, revErr := sessionContentRevision(path)
	if revErr != nil {
		baseRevision = 0
	}
	if len(entries) == 0 {
		s.adoptDAGPosition(st, plan)
		s.markCheckpointPersisted(path, digest, version, baseRevision, rewriteVersion, msgs, true)
		return nil
	}
	tail := st.lastGoodEnd
	if _, err := appendSessionDAGEntriesUnlocked(path, entries); err != nil {
		s.requeuePendingMarkers(pending)
		return err
	}
	st.damaged = false
	if err := st.replayFrom(ctx, tail, defaultSessionReplayLimits); err != nil {
		return err
	}
	if st.damaged || st.heads[plan.head] == nil {
		return fmt.Errorf("session log %s: entries appended without the lock did not replay", path)
	}
	plan.applyIDRenames(s)
	s.adoptDAGPosition(st, plan)
	s.markCheckpointPersisted(path, digest, version, baseRevision, rewriteVersion, msgs, true)
	return nil
}

// dagStateWithoutLock replays the log for an unlocked append. A torn tail is
// left alone: repairing it needs the lock, and the append terminates it
// instead so its own entries start on a fresh line.
func (s *Session) dagStateWithoutLock(ctx context.Context, path string) (*sessionDAGState, error) {
	logPath := store.SessionEventLog(path)
	s.mu.RLock()
	cached := s.head.state
	s.mu.RUnlock()
	header, ok, err := readSessionDAGHeader(path)
	if err != nil {
		return nil, err
	}
	if cached != nil && ok && cached.path == logPath && header.generation == cached.generation {
		if info, err := os.Stat(logPath); err == nil && info.Size() >= cached.lastGoodEnd {
			cached.damaged = false
			if err := cached.replayFrom(ctx, cached.lastGoodEnd, defaultSessionReplayLimits); err == nil {
				return cached, nil
			}
		}
	}
	return replaySessionDAG(ctx, logPath, defaultSessionReplayLimits)
}

// shutdownHeadBatch turns a save plan into an unlocked batch. Message entries
// (and an owned rewind, which becomes the fork point) move onto a fresh
// concurrent head, so a locked writer continuing the old head can never
// interleave with them. A turn_end whose turn_begin is already on the old
// head stays there to close it; a batch of overlays and markers alone needs
// no new head. plan is updated to describe the batch actually written.
func shutdownHeadBatch(plan *dagWritePlan, pending []sessionDAGEntry, now time.Time) []sessionDAGEntry {
	from, moveHead := "", false
	for _, e := range plan.entries {
		switch e.Type {
		case sessionDAGTypeMessage:
			if !moveHead {
				from = e.Parent
			}
			moveHead = true
		case sessionDAGTypeRewind:
			from, moveHead = e.To, true
		}
	}
	if plan.forked || !moveHead {
		for i := range pending {
			pending[i].Head = plan.head
		}
		return append(plan.entries, pending...)
	}
	oldHead, newHead := plan.head, NewHeadID()
	out := make([]sessionDAGEntry, 0, len(plan.entries)+len(pending)+1)
	out = append(out, sessionDAGEntry{Type: sessionDAGTypeFork, Head: oldHead, NewHead: newHead, From: from, Kind: HeadKindConcurrent, At: now})
	for _, e := range plan.entries {
		if e.Type == sessionDAGTypeRewind {
			continue
		}
		e.Head = newHead
		out = append(out, e)
	}
	begun := map[string]bool{}
	for _, e := range pending {
		if e.Type == sessionDAGTypeTurnBegin {
			begun[e.Turn] = true
		}
	}
	for _, e := range pending {
		e.Head = newHead
		if e.Type == sessionDAGTypeTurnEnd && !begun[e.Turn] {
			e.Head = oldHead
		}
		out = append(out, e)
	}
	plan.head, plan.forked, plan.rewound, plan.pureAppend = newHead, true, false, false
	return out
}

// republishDAGDerivedIfPending refreshes the derived files a previous
// unlocked or checkpoint save skipped, once a locked save finds nothing new
// to append.
func (s *Session) republishDAGDerivedIfPending(ctx context.Context, path string, st *sessionDAGState, plan *dagWritePlan, msgs []provider.Message, digest [sha256.Size]byte, revision int64) {
	if !s.persistState(path).projectionPending {
		return
	}
	selected := st.selectedHead()
	displayCurrent := selected == plan.head && writeDAGCheckpointCache(path, plan, msgs, revision)
	s.publishDAGDerived(ctx, path, st, plan, msgs, digest, revision, selected, displayCurrent, false)
}

// DerivedFilesPending reports whether the last save of path left its derived
// files (listing sidecar, display cache, head index) to a later locked save,
// as an unlocked shutdown append or a tool checkpoint does.
func (s *Session) DerivedFilesPending(path string) bool {
	return s.persistState(path).projectionPending
}
