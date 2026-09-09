package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

type dagRoute int

const (
	dagRouteSchemaOne dagRoute = iota
	dagRouteNative
	dagRouteUpgrade
)

// SessionLogSchemaEnv is the emergency switch back to the schema-1 writer for
// sessions that have not been upgraded yet; upgraded logs stay schema 2.
const SessionLogSchemaEnv = "REASONIX_SESSION_LOG"

func dagWriterEnabled() bool {
	return os.Getenv(SessionLogSchemaEnv) != "v1"
}

// probeLogForSave classifies the event log for a save, refusing a schema this
// build cannot own and healing a torn schema-1 tail before anything appends
// behind it.
func probeLogForSave(path string) (sessionEventLogProbe, error) {
	probe, err := probeSessionEventLog(path)
	if err != nil {
		return probe, err
	}
	if probe.futureSchema {
		return probe, fmt.Errorf("session event log for %s uses schema %d; this build supports up to %d", path, probe.schemaVersion, sessionDAGSchemaVersion)
	}
	if probe.native && probe.size > 0 {
		if err := repairSessionEventLogTail(path); err != nil {
			return probe, fmt.Errorf("repair session event log: %w", err)
		}
	}
	return probe, nil
}

// dagSaveRoute decides whether a save runs on the schema-2 path. An existing
// schema-1 log is only upgraded by the lease holder; a session with no log
// yet (new, or a bare checkpoint) starts schema 2 unless another runtime
// holds it. Foreign files and overlong names stay on the schema-1 path.
func (s *Session) dagSaveRoute(path string, probe sessionEventLogProbe) dagRoute {
	if probe.dag {
		return dagRouteNative
	}
	if !dagWriterEnabled() || !probe.native {
		return dagRouteSchemaOne
	}
	if probe.size > 0 {
		if !SessionLeaseHeldByCurrentRuntime(path) {
			return dagRouteSchemaOne
		}
		return dagRouteUpgrade
	}
	if SessionLeaseHeldByOtherRuntime(path) {
		return dagRouteSchemaOne
	}
	return dagRouteUpgrade
}

// saveDAGLocked persists msgs to a schema-2 log: it replays (or extends) the
// cached graph, diffs the in-memory transcript against this session's head,
// appends the resulting entries in one batch, and refreshes the derived
// files. Concurrent writers never conflict; a writer that only fell behind
// its own head reports a stale-prefix conflict so the caller adopts disk.
func (s *Session) saveDAGLocked(path string, mode sessionSaveMode, route dagRoute, msgs []provider.Message, version uint64, rewriteVersion int, digest [sha256.Size]byte) error {
	ctx := context.Background()
	now := time.Now().UTC()
	baseRevision, _, err := sessionContentRevision(path)
	ledgerUnreadable := err != nil
	if ledgerUnreadable {
		// A persisted session with an unreadable ledger fails closed, as in
		// schema 1; a brand-new transcript still lands first and the record
		// step below reports the sidecar.
		if sessionArtifactsHaveContent(path) {
			return err
		}
		baseRevision = 0
	}
	var st *sessionDAGState
	if route == dagRouteUpgrade {
		st, err = s.upgradeLogForSave(ctx, path, now)
	} else {
		st, err = s.dagStateForSave(ctx, path, now)
	}
	if err != nil {
		return err
	}
	s.ensureMessageIDsForSave(msgs)
	plan, err := s.planDAGWrite(path, st, msgs, mode, now)
	if err != nil {
		return err
	}
	deferProjection := mode.defersProjection()
	pending := s.takePendingMarkers()
	for i := range pending {
		pending[i].Head = plan.head
	}
	plan.entries = append(plan.entries, pending...)
	if len(plan.entries) == 0 {
		s.adoptDAGPosition(st, plan)
		s.republishDAGDerivedIfPending(ctx, path, st, plan, msgs, digest, baseRevision)
		s.maintainDAGLog(ctx, path, st, plan, mode, now)
		s.markCheckpointPersisted(path, digest, version, baseRevision, rewriteVersion, msgs, deferProjection)
		return nil
	}
	reserved, err := invalidateSessionListingProjection(path)
	if err != nil {
		if !ledgerUnreadable {
			return fmt.Errorf("invalidate session listing projection: %w", err)
		}
		reserved = 0
	}
	tail := st.lastGoodEnd
	if _, err := appendSessionDAGEntries(path, plan.entries, true); err != nil {
		s.requeuePendingMarkers(pending)
		return err
	}
	if err := st.replayFrom(ctx, tail, defaultSessionReplayLimits); err != nil {
		return err
	}
	if st.damaged {
		return fmt.Errorf("session log %s: appended entries did not replay", path)
	}
	plan.applyIDRenames(s)
	s.adoptDAGPosition(st, plan)
	// The compatibility checkpoint lands before the ledger, as in schema 1, so
	// a metadata failure never leaves the anchor behind the log.
	selected := st.selectedHead()
	displayCurrent := selected == plan.head && writeDAGCheckpointCache(path, plan, msgs, baseRevision)
	revision, err := recordSessionContentRevision(path, digest, baseRevision, reserved)
	if err != nil {
		return err
	}
	s.publishDAGDerived(ctx, path, st, plan, msgs, digest, revision, selected, displayCurrent, deferProjection)
	s.maintainDAGLog(ctx, path, st, plan, mode, now)
	s.markCheckpointPersisted(path, digest, version, revision, rewriteVersion, msgs, deferProjection)
	return nil
}

// dagStateForSave returns the replayed graph, extending the cached state by
// the bytes appended since its last observed tail when the generation and
// size still allow it, and settles a torn tail before any append.
func (s *Session) dagStateForSave(ctx context.Context, path string, now time.Time) (*sessionDAGState, error) {
	logPath := store.SessionEventLog(path)
	s.mu.RLock()
	cached := s.head.state
	s.mu.RUnlock()
	header, ok, err := readSessionDAGHeader(path)
	if err != nil {
		return nil, err
	}
	var st *sessionDAGState
	if cached != nil && ok && cached.path == logPath && header.generation == cached.generation && !cached.damaged {
		if info, err := os.Stat(logPath); err == nil && info.Size() >= cached.lastGoodEnd {
			st = cached
			if err := st.replayFrom(ctx, st.lastGoodEnd, defaultSessionReplayLimits); err != nil {
				return nil, err
			}
		}
	}
	if st == nil {
		st, err = replaySessionDAG(ctx, logPath, defaultSessionReplayLimits)
		if err != nil {
			return nil, err
		}
	}
	if st.damaged {
		if err := settleDAGTail(ctx, path, st, now); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// settleDAGTail waits out the quiet window a torn tail might still be
// finishing, then repairs it; appending after an unrepaired partial line
// would bury this writer's own entry inside it.
func settleDAGTail(ctx context.Context, path string, st *sessionDAGState, now time.Time) error {
	logPath := store.SessionEventLog(path)
	info, err := os.Stat(logPath)
	if err != nil {
		return err
	}
	if age := now.Sub(info.ModTime()); age < sessionDAGTailRepairMinAge {
		time.Sleep(sessionDAGTailRepairMinAge - age)
		st.damaged = false
		if err := st.replayFrom(ctx, st.lastGoodEnd, defaultSessionReplayLimits); err != nil {
			return err
		}
		if !st.damaged {
			return nil
		}
	}
	repaired, err := repairSessionDAGTail(path, st, time.Now().UTC())
	if err != nil {
		return err
	}
	if !repaired {
		return fmt.Errorf("session log %s has a torn tail that is still being written", path)
	}
	return nil
}

// upgradeLogForSave replaces the schema-1 log (or bare checkpoint) with
// generation 1 of a schema-2 log built from the transcript on disk. The
// in-memory delta is then written by the ordinary diff, exactly like any
// other save. Callers hold the file lock and satisfied dagSaveRoute.
func (s *Session) upgradeLogForSave(ctx context.Context, path string, now time.Time) (*sessionDAGState, error) {
	disk, err := loadSessionTranscript(ctx, path, defaultSessionReplayLimits, nil)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	msgs := migrateLegacyProviderContent(NormalizeSession(disk.msgs))
	assignLegacyMessageIDs(path, msgs)
	var inFlight *InFlightTurnMeta
	if meta, ok, err := LoadBranchMeta(path); err == nil && ok {
		inFlight = meta.InFlightTurn
	}
	if err := upgradeSessionLogToDAG(path, msgs, disk.times, inFlight, now); err != nil {
		return nil, err
	}
	st, err := replaySessionDAG(ctx, store.SessionEventLog(path), defaultSessionReplayLimits)
	if err != nil {
		return nil, err
	}
	if len(msgs) > 0 {
		slog.Info("session: upgraded event log to schema 2", "path", path, "messages", len(msgs))
	}
	return st, nil
}

// ensureMessageIDsForSave mints ids for messages that reached the session
// without one and writes them back by position so the next save sees the
// same ids the log now holds.
func (s *Session) ensureMessageIDsForSave(msgs []provider.Message) {
	minted := false
	for i := range msgs {
		if msgs[i].ID == "" {
			msgs[i].ID = NewMessageID()
			minted = true
		}
	}
	if !minted {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range msgs {
		if i < len(s.Messages) && s.Messages[i].ID == "" && messagesEqualForStorage(s.Messages[i], msgs[i]) {
			s.Messages[i].ID = msgs[i].ID
		}
	}
}

func (s *Session) adoptDAGPosition(st *sessionDAGState, plan *dagWritePlan) {
	h := st.heads[plan.head]
	leaf := ""
	if h != nil {
		leaf = h.leaf
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.head.ref = HeadRef{HeadID: plan.head, LeafID: leaf, LogGeneration: st.generation, LogOffset: st.lastGoodEnd}
	s.head.dag = true
	s.head.state = st
	s.head.headCount = len(st.heads)
	if plan.forked {
		s.head.events = append(s.head.events, HeadEvent{Kind: HeadEventForkedConcurrent, HeadID: plan.head, OtherWriter: plan.otherWriter})
	}
}

// writeDAGCheckpointCache refreshes the .jsonl random-read model for the
// selected head, extending it in place for a pure append. It reports whether
// the cache now matches msgs; failures are logged, never fatal.
func writeDAGCheckpointCache(path string, plan *dagWritePlan, msgs []provider.Message, baseRevision int64) bool {
	if plan.pureAppend {
		current, err := appendSessionDisplayReadModel(path, msgs, plan.appendFrom, baseRevision)
		if err != nil {
			slog.Warn("session: keeping save after display read-model append failure", "path", path, "err", err)
		}
		if current {
			return true
		}
	}
	if err := writeSessionMessages(path, msgs); err != nil {
		slog.Warn("session: keeping save after display read-model write failure", "path", path, "err", err)
		return false
	}
	return true
}

// publishDAGDerived refreshes the display index when the .jsonl cache is
// current for this head, and the head index plus meta mirror on every save.
func (s *Session) publishDAGDerived(ctx context.Context, path string, st *sessionDAGState, plan *dagWritePlan, msgs []provider.Message, digest [sha256.Size]byte, revision int64, selected string, displayCurrent, deferProjection bool) {
	if displayCurrent {
		appendFrom := -1
		if plan.pureAppend {
			appendFrom = plan.appendFrom
		}
		if err := refreshCheckpointDisplayIndex(path, msgs, digest, revision, appendFrom, deferProjection); err != nil {
			slog.Warn("session: keeping save after display index write failure", "path", path, "err", err)
		}
	}
	if err := writeSessionDAGIndex(ctx, path, st); err != nil {
		slog.Warn("session: keeping save after head index write failure", "path", path, "err", err)
	}
	if err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		meta.HeadID = selected
		meta.HeadCount = len(st.heads)
		meta.LogSchema = sessionDAGSchemaVersion
		meta.LogGeneration = st.generation
		return nil
	}); err != nil {
		slog.Warn("session: head metadata update deferred", "path", path, "err", err)
	}
}

// maintainDAGLog rotates the log when it has outgrown its live chains or a
// redaction needs its bytes physically erased, but only under the
// single-writer proof; otherwise the log simply keeps growing for now.
func (s *Session) maintainDAGLog(ctx context.Context, path string, st *sessionDAGState, plan *dagWritePlan, mode sessionSaveMode, now time.Time) {
	if mode != sessionSaveRewriteCompact && st.holes == 0 && !sessionDAGLogOversized(st) {
		return
	}
	if err := sessionDAGSingleWriterProof(path, st, now); err != nil {
		slog.Info("session: log rotation deferred", "path", path, "reason", err)
		return
	}
	if err := rotateSessionDAG(path, st, now); err != nil {
		slog.Warn("session: log rotation failed", "path", path, "err", err)
		return
	}
	fresh, err := replaySessionDAG(ctx, store.SessionEventLog(path), defaultSessionReplayLimits)
	if err != nil {
		slog.Warn("session: replay after rotation failed", "path", path, "err", err)
		return
	}
	s.adoptDAGPosition(fresh, &dagWritePlan{head: plan.head})
	if err := writeSessionDAGIndex(ctx, path, fresh); err != nil {
		slog.Warn("session: head index write after rotation failed", "path", path, "err", err)
	}
}
