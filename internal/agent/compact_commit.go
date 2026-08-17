package agent

import (
	"errors"
	"fmt"
	"time"

	"reasonix/internal/provider"
)

type summaryProjectionCommit struct {
	canonical, fold, projected                       []provider.Message
	result                                           foldSummary
	transcriptVersion, projectionVersion, generation uint64
	activeTurn                                       int64
	trigger, summary, inputHash, outputHash          string
	sourceTokens, projectionTokens                   int
	// covered is the canonical length the frozen projection body represents;
	// messages past it splice live from the transcript.
	covered int
}

// commitSummaryProjection CAS-installs a checkpoint under compactionMu:
// transcript version/hash, projection version, and generation must still match.
// The maintenance event is emitted only after the lock is released so a sink
// that re-enters ContextMaintenanceSnapshot cannot deadlock.
func (a *Agent) commitSummaryProjection(commit summaryProjectionCommit) (CompactionState, error) {
	state := a.summaryProjectionState(commit)
	a.sess.compactionMu.Lock()
	current, currentVersion := a.sess.conversation.snapshotMessagesVersion()
	if currentVersion != commit.transcriptVersion ||
		len(current) != len(commit.canonical) ||
		coveredPrefixHash(current, len(current)) != coveredPrefixHash(commit.canonical, len(commit.canonical)) ||
		a.sess.compactionState.Projection.ProjectionVersion != commit.projectionVersion ||
		a.sess.compactionState.Generation != commit.generation {
		a.sess.compactionMu.Unlock()
		return CompactionState{}, errCompressStaleContext
	}
	prev := a.sess.compactionState
	a.sess.compactionState = state
	if err := a.persistCompactionStateLocked(); err != nil {
		a.sess.compactionState = prev
		a.sess.compactionMu.Unlock()
		if errors.Is(err, errCompressStaleContext) {
			return CompactionState{}, err
		}
		return CompactionState{}, fmt.Errorf("persist projection: %w", err)
	}
	a.sess.checkpointState = "applied"
	if commit.activeTurn != 0 && commit.trigger != CompactionTriggerManual {
		a.sess.compaction.lastTurn.Store(commit.activeTurn)
	}
	receipt := state.LastReceipt
	a.sess.compactionMu.Unlock()
	a.emitContextMaintenance(receipt)
	return state, nil
}

func (a *Agent) summaryProjectionState(commit summaryProjectionCommit) CompactionState {
	projectionVersion := commit.projectionVersion + 1
	now := time.Now().UTC()
	summaryHash := summaryContentHash(commit.summary)
	coveredHash := coveredPrefixHash(commit.canonical, commit.covered)
	receipt := &ContextMaintenanceReceipt{
		OperationID: fmt.Sprintf("summary-%d-%s", projectionVersion, commit.outputHash), Status: "applied",
		Action: "summary", Trigger: commit.trigger, SourceProjection: commit.projectionVersion,
		ProjectionVersion: projectionVersion, CoveredCount: commit.covered, CoveredPrefixHash: coveredHash,
		InputHash: commit.inputHash, OutputHash: commit.outputHash, InputTokens: commit.sourceTokens,
		ResultTokens: commit.projectionTokens, SavedTokens: max(0, commit.sourceTokens-commit.projectionTokens),
		SummaryHash: summaryHash, CacheBreak: true, CreatedAt: now,
	}
	// LastReceipt is authoritative; do not mirror last_trigger/last_mode/token
	// counters or top-level blocked_* fields (stripped again on save).
	return CompactionState{
		SchemaVersion: compactionStateSchemaCurrent, TranscriptVersion: commit.transcriptVersion,
		Generation: commit.generation + 1, PromptCacheKey: a.currentPromptCacheKey(),
		Projection: ContextProjection{
			Messages: commit.projected, TranscriptVersion: commit.transcriptVersion,
			ProjectionVersion: projectionVersion, CoveredCount: commit.covered, CoveredPrefixHash: coveredHash,
			SummaryHash: summaryHash, SourceTokens: commit.sourceTokens, ProjectionTokens: commit.projectionTokens,
			ViewInputHash: commit.inputHash, ViewOutputHash: commit.outputHash, CreatedAt: now,
		},
		LastReceipt: receipt, UpdatedAt: now,
	}
}
