package agent

import (
	"errors"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"time"

	"reasonix/internal/contract/provider"
)

type summaryProjectionCommit struct {
	canonical, fold, projected []provider.Message
	// covered is the canonical prefix this fold claims, decided by the caller
	// that planned the fold. Persistence records it and never re-derives one:
	// two places computing a boundary is two contracts waiting to disagree.
	covered                                          int
	result                                           foldSummary
	transcriptVersion, projectionVersion, generation uint64
	activeTurn                                       int64
	trigger, summary, inputHash, outputHash          string
	sourceTokens, projectionTokens                   int
	// summaryUsage is what the transaction spent producing this projection,
	// which has nothing to do with sourceTokens above.
	summaryUsage sessionstore.CompactionUsage
}

// commitSummaryProjection CAS-installs a checkpoint under compactionMu:
// transcript version/hash, projection version, and generation must still match.
// The maintenance event is emitted only after the lock is released so a sink
// that re-enters ContextMaintenanceSnapshot cannot deadlock.
func (a *contextWindow) commitSummaryProjection(commit summaryProjectionCommit) (sessionstore.CompactionState, error) {
	if commit.covered <= 0 || commit.covered > len(commit.canonical) {
		return sessionstore.CompactionState{}, fmt.Errorf("compaction commit: covered %d outside canonical length %d",
			commit.covered, len(commit.canonical))
	}
	state := a.summaryProjectionState(commit)
	a.sess.win.compactionMu.Lock()
	current, currentVersion := a.sess.conversation.SnapshotMessagesVersion()
	if currentVersion != commit.transcriptVersion ||
		len(current) != len(commit.canonical) ||
		sessionstore.CoveredPrefixHash(current, len(current)) != sessionstore.CoveredPrefixHash(commit.canonical, len(commit.canonical)) ||
		a.sess.win.compactionState.Projection.ProjectionVersion != commit.projectionVersion ||
		a.sess.win.compactionState.Generation != commit.generation {
		a.sess.win.compactionMu.Unlock()
		return sessionstore.CompactionState{}, errCompressStaleContext
	}
	prev := a.sess.win.compactionState
	a.sess.win.compactionState = state
	if err := a.persistCompactionStateLocked(); err != nil {
		a.sess.win.compactionState = prev
		a.sess.win.compactionMu.Unlock()
		if errors.Is(err, errCompressStaleContext) {
			return sessionstore.CompactionState{}, err
		}
		return sessionstore.CompactionState{}, fmt.Errorf("persist projection: %w: %w", errProjectionNotPersisted, err)
	}
	a.sess.win.checkpointState = "applied"
	receipt := state.LastReceipt
	a.sess.win.compactionMu.Unlock()
	// The installed projection carries the step ids, either in what it kept or
	// in the note the fold appended, so the next round owes nothing.
	a.emitContextMaintenance(receipt)
	return state, nil
}

func (a *contextWindow) summaryProjectionState(commit summaryProjectionCommit) sessionstore.CompactionState {
	projectionVersion := commit.projectionVersion + 1
	now := time.Now().UTC()
	summaryHash := summaryContentHash(commit.summary)
	coveredHash := sessionstore.CoveredPrefixHash(commit.canonical, commit.covered)
	receipt := &sessionstore.ContextMaintenanceReceipt{
		OperationID: fmt.Sprintf("summary-%d-%s", projectionVersion, commit.outputHash), Status: "applied",
		Action: "summary", Trigger: commit.trigger, SourceProjection: commit.projectionVersion,
		ProjectionVersion: projectionVersion, CoveredCount: commit.covered, CoveredPrefixHash: coveredHash,
		InputHash: commit.inputHash, OutputHash: commit.outputHash, InputTokens: commit.sourceTokens,
		ResultTokens: commit.projectionTokens, SavedTokens: max(0, commit.sourceTokens-commit.projectionTokens),
		SummaryHash: summaryHash, CacheBreak: true, CreatedAt: now,
		SummaryUsage: commit.summaryUsage,
	}
	// LastReceipt is authoritative; do not mirror last_trigger/last_mode/token
	// counters or top-level blocked_* fields (stripped again on save).
	return sessionstore.CompactionState{
		SchemaVersion: sessionstore.CompactionStateSchemaCurrent, TranscriptVersion: commit.transcriptVersion,
		Generation: commit.generation + 1, PromptCacheKey: a.currentPromptCacheKey(),
		Projection: sessionstore.ContextProjection{
			Messages: commit.projected, TranscriptVersion: commit.transcriptVersion,
			ProjectionVersion: projectionVersion, CoveredCount: commit.covered, CoveredPrefixHash: coveredHash,
			SummaryHash: summaryHash, SourceTokens: commit.sourceTokens, ProjectionTokens: commit.projectionTokens,
			ViewInputHash: commit.inputHash, ViewOutputHash: commit.outputHash, CreatedAt: now,
		},
		LastReceipt: receipt, UpdatedAt: now,
	}
}
