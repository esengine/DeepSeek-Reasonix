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
}

// commitSummaryProjection performs the final CAS, content-addressed archive,
// and sidecar switch after all network and interceptor work has completed.
func (a *Agent) commitSummaryProjection(commit summaryProjectionCommit) (CompactionState, error) {
	current, currentVersion := a.session.snapshotMessagesVersion()
	a.compactionMu.Lock()
	currentProjectionVersion := a.compactionState.Projection.ProjectionVersion
	currentGeneration := a.compactionState.Generation
	a.compactionMu.Unlock()
	if currentVersion != commit.transcriptVersion || len(current) != len(commit.canonical) ||
		coveredPrefixHash(current, len(current)) != coveredPrefixHash(commit.canonical, len(commit.canonical)) ||
		currentProjectionVersion != commit.projectionVersion || currentGeneration != commit.generation {
		return CompactionState{}, errCompressStaleContext
	}

	archive := ""
	if a.archiveDir != "" {
		path, err := archiveMessages(a.archiveDir, commit.fold)
		if err != nil {
			return CompactionState{}, fmt.Errorf("archive: %w", err)
		}
		archive = path
	}
	state := a.summaryProjectionState(commit, archive)
	if err := a.installProjectionIfCurrent(state, commit.projectionVersion, commit.generation); err != nil {
		if errors.Is(err, errCompressStaleContext) {
			return CompactionState{}, err
		}
		return CompactionState{}, fmt.Errorf("persist projection: %w", err)
	}
	if commit.activeTurn != 0 && commit.trigger != CompactionTriggerManual {
		a.lastCompactionTurn.Store(commit.activeTurn)
	}
	a.session.NoteContentRewrite("compact_" + commit.trigger)
	a.emitContextMaintenance(state.LastReceipt)
	return state, nil
}

func (a *Agent) summaryProjectionState(commit summaryProjectionCommit, archive string) CompactionState {
	projectionVersion := commit.projectionVersion + 1
	now := time.Now().UTC()
	summaryHash := summaryContentHash(commit.summary)
	coveredHash := coveredPrefixHash(commit.canonical, len(commit.canonical))
	receipt := &ContextMaintenanceReceipt{
		OperationID: fmt.Sprintf("summary-%d-%s", projectionVersion, commit.outputHash), Status: "applied",
		Action: "summary", Trigger: commit.trigger, SourceProjection: commit.projectionVersion,
		ProjectionVersion: projectionVersion, CoveredCount: len(commit.canonical), CoveredPrefixHash: coveredHash,
		InputHash: commit.inputHash, OutputHash: commit.outputHash, InputTokens: commit.sourceTokens,
		ResultTokens: commit.projectionTokens, SavedTokens: max(0, commit.sourceTokens-commit.projectionTokens),
		SummaryHash: summaryHash, Archive: archive, CacheBreak: true, CreatedAt: now,
	}
	state := CompactionState{
		SchemaVersion: compactionStateSchemaCurrent, TranscriptVersion: commit.transcriptVersion,
		Generation: commit.generation + 1, PromptCacheKey: a.currentPromptCacheKey(),
		NativeContextEditingAccepted: a.nativeContextEditingAccepted.Load(),
		ContextEditingFallbackLocal:  a.contextEditingRuntimeFallback.Load(),
		Projection: ContextProjection{
			Messages: commit.projected, TranscriptVersion: commit.transcriptVersion,
			ProjectionVersion: projectionVersion, CoveredCount: len(commit.canonical), CoveredPrefixHash: coveredHash,
			SummaryHash: summaryHash, SourceTokens: commit.sourceTokens, ProjectionTokens: commit.projectionTokens,
			ViewInputHash: commit.inputHash, ViewOutputHash: commit.outputHash, CreatedAt: now,
		},
		LastCacheState: a.CacheState(), LastTrigger: commit.trigger, LastMode: commit.result.Mode,
		LastSourceTokens: commit.sourceTokens, LastResultTokens: commit.projectionTokens,
		LastReceipt: receipt, UpdatedAt: now,
	}
	if a.pricing != nil && commit.result.Usage != nil {
		state.LastCompactionCost = a.pricing.Cost(commit.result.Usage)
	}
	return state
}
