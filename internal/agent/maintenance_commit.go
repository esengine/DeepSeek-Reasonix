package agent

import (
	"errors"
	"fmt"
	"time"

	"reasonix/internal/provider"
)

// maintenanceInstall is one free projection rewrite (no summarizer call): the
// visible view it started from and the projected view replacing it.
type maintenanceInstall struct {
	trigger, action    string
	state              CompactionState
	canonical          []provider.Message
	transcriptVersion  uint64
	visible, projected []provider.Message
	affected           int
}

// installMaintenanceProjection CAS-installs a free projection under
// compactionMu. The caller owns compactionRunMu for the whole maintenance run;
// canonical storage, including RawContent, is never modified.
func (a *Agent) installMaintenanceProjection(in maintenanceInstall) (bool, error) {
	projected := projectionMessagesPreservingPinnedContext(in.projected)
	projected, _, err := rebasePinnedContextProjection(projected, in.canonical, len(in.canonical))
	if err != nil {
		return false, err
	}
	sourceTokens := a.estimatedVisibleRequestTokens(in.visible)
	resultTokens := a.estimatedVisibleRequestTokens(projected)
	inputHash := a.contextMaintenanceInputHash(modelInputMessages(in.visible))
	outputHash := providerVisibleFingerprint(modelInputMessages(projected))
	projectionVersion := in.state.Projection.ProjectionVersion + 1
	now := time.Now().UTC()
	coveredHash := coveredPrefixHash(in.canonical, len(in.canonical))
	receipt := &ContextMaintenanceReceipt{
		OperationID: fmt.Sprintf("%s-%d-%s", in.action, projectionVersion, outputHash), Status: "applied", Action: in.action,
		Trigger: in.trigger, SourceProjection: in.state.Projection.ProjectionVersion, ProjectionVersion: projectionVersion,
		CoveredCount: len(in.canonical), CoveredPrefixHash: coveredHash, InputHash: inputHash, OutputHash: outputHash,
		InputTokens: sourceTokens, ResultTokens: resultTokens, SavedTokens: max(0, sourceTokens-resultTokens),
		AffectedToolResults: in.affected, CacheBreak: true, CreatedAt: now,
	}
	next := in.state
	next.SchemaVersion = compactionStateSchemaCurrent
	next.TranscriptVersion = in.transcriptVersion
	next.Generation++
	next.PromptCacheKey = a.currentPromptCacheKey()
	next.Projection = ContextProjection{
		Messages: projected, TranscriptVersion: in.transcriptVersion, ProjectionVersion: projectionVersion,
		CoveredCount: len(in.canonical), CoveredPrefixHash: coveredHash, SourceTokens: sourceTokens,
		PinnedContextHash: pinnedContextCoverageHash(in.canonical, len(in.canonical)),
		ProjectionTokens:  resultTokens, ViewInputHash: inputHash, ViewOutputHash: outputHash, CreatedAt: now,
	}
	next.LastReceipt = receipt
	next.UpdatedAt = now

	a.sess.compactionMu.Lock()
	current, currentVersion := a.sess.conversation.snapshotMessagesVersion()
	if currentVersion != in.transcriptVersion || len(current) != len(in.canonical) ||
		coveredPrefixHash(current, len(current)) != coveredHash ||
		a.sess.compactionState.Projection.ProjectionVersion != in.state.Projection.ProjectionVersion ||
		a.sess.compactionState.Generation != in.state.Generation {
		a.sess.compactionMu.Unlock()
		return false, errCompressStaleContext
	}
	previous := a.sess.compactionState
	a.sess.compactionState = next
	if err := a.persistCompactionStateLocked(); err != nil {
		a.sess.compactionState = previous
		a.sess.compactionMu.Unlock()
		if errors.Is(err, errCompressStaleContext) {
			return false, err
		}
		return false, fmt.Errorf("persist %s projection: %w", in.action, err)
	}
	a.sess.checkpointState = "applied"
	a.sess.compactionMu.Unlock()
	a.emitContextMaintenance(receipt)
	return true, nil
}
