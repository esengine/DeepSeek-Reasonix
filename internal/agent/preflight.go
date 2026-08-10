package agent

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"reasonix/internal/provider"
)

// ErrCompactionRequired is returned when the prompt exceeds the provider limit
// and compaction could not produce a usable projection. Callers may retry.
var ErrCompactionRequired = errors.New("context exceeds provider limit and compaction failed")

// modelVisibleMessages returns the provider-bound message list: a valid
// projection plus any post-projection appends, otherwise the full canonical
// transcript. LocalOnly stripping still happens in prepareSamplingRequest.
func (a *Agent) modelVisibleMessages() []provider.Message {
	if a == nil || a.session == nil {
		return nil
	}
	msgs, version := a.session.snapshotMessagesVersion()
	a.compactionMu.Lock()
	st := a.compactionState
	a.compactionMu.Unlock()
	if projectionValid(st, msgs, version, a.currentPromptCacheKey()) {
		if visible := modelVisibleFromProjection(st.Projection, msgs); len(visible) > 0 {
			return visible
		}
	}
	return msgs
}

func (a *Agent) currentProjectionVersion() uint64 {
	if a == nil {
		return 0
	}
	a.compactionMu.Lock()
	defer a.compactionMu.Unlock()
	return a.compactionState.Projection.ProjectionVersion
}

// currentPromptCacheKey is the lineage key for the bound session + model.
func (a *Agent) currentPromptCacheKey() string {
	if a == nil {
		return ""
	}
	a.compactionMu.Lock()
	defer a.compactionMu.Unlock()
	return a.currentPromptCacheKeyLocked()
}

func (a *Agent) currentPromptCacheKeyLocked() string {
	key := promptCacheKey(a.workspaceID, BranchID(a.sessionPath), a.modelRef)
	return key + a.contextEditingLineageSuffixLocked()
}

// InvalidateProjection drops the in-memory and on-disk projection after
// lineage-changing operations (rewind, branch, fork, system/model change).
func (a *Agent) InvalidateProjection() {
	if a == nil {
		return
	}
	a.compactionMu.Lock()
	path := a.sessionPath
	a.compactionState = CompactionState{}
	a.compactionMu.Unlock()
	a.compactStuck = false
	a.consecutiveCompacts = 0
	a.lastCompactionTurn.Store(0)
	if path != "" {
		if err := RemoveCompactionState(path); err != nil {
			slog.Warn("agent: remove context projection", "err", err)
		}
	}
}

// LoadProjectionSidecar loads the context sidecar into the agent. Corrupt or
// incompatible state is dropped so the next request rebuilds from canonical.
// Sidecars whose PromptCacheKey does not match the current agent lineage are
// discarded without deleting the file (another model may still own it).
func (a *Agent) LoadProjectionSidecar(sessionPath string) {
	if a == nil {
		return
	}
	_, effective, policyVersion := resolveContextEditing(a.requestedContextEditing, a.prov)
	a.compactionMu.Lock()
	a.sessionPath = sessionPath
	a.contextEditing = effective
	a.contextEditingPolicyVersion = policyVersion
	a.compactionState = CompactionState{}
	a.compactionMu.Unlock()
	a.nativeContextEditingAccepted.Store(false)
	a.contextEditingRuntimeFallback.Store(false)
	if sessionPath == "" {
		a.resetCompactionState()
		return
	}
	st, ok, err := LoadCompactionState(sessionPath)
	if err != nil {
		slog.Warn("agent: load context projection", "err", err)
		_ = RemoveCompactionState(sessionPath)
		a.resetCompactionState()
		return
	}
	if !ok {
		a.resetCompactionState()
		return
	}
	a.compactionMu.Lock()
	if st.ContextEditingFallbackLocal && !st.NativeContextEditingAccepted && a.contextEditing == "native" {
		a.contextEditing = "local"
		a.contextEditingRuntimeFallback.Store(true)
	}
	// Fail closed: known lineage requires an exact stored key (including
	// rejecting blank keys on early sidecars written before this field).
	key := a.currentPromptCacheKeyLocked()
	if (key != "" && st.PromptCacheKey != key) ||
		(st.Projection.CoveredPrefixHash == "" && st.BlockedInputHash == "" &&
			!st.ContextEditingFallbackLocal && !st.NativeContextEditingAccepted) {
		a.contextEditing = effective
		a.contextEditingPolicyVersion = policyVersion
		a.compactionState = CompactionState{}
		a.contextEditingRuntimeFallback.Store(false)
		a.compactionMu.Unlock()
		return
	}
	a.compactionState = st
	a.compactionMu.Unlock()
	a.nativeContextEditingAccepted.Store(st.NativeContextEditingAccepted)
}

func (a *Agent) resetCompactionState() {
	a.compactionMu.Lock()
	a.compactionState = CompactionState{}
	a.compactionMu.Unlock()
}

// BindSessionPath rebinds projection persistence to path. When loadSidecar is
// true the existing sidecar is loaded (resume/switch); otherwise in-memory
// projection is cleared without deleting another session's sidecar file.
func (a *Agent) BindSessionPath(path string, loadSidecar bool) {
	if a == nil {
		return
	}
	if loadSidecar {
		a.LoadProjectionSidecar(path)
		return
	}
	_, effective, policyVersion := resolveContextEditing(a.requestedContextEditing, a.prov)
	a.compactionMu.Lock()
	a.sessionPath = path
	a.contextEditing = effective
	a.contextEditingPolicyVersion = policyVersion
	a.compactionState = CompactionState{}
	a.cacheState = CacheStateUnknown
	a.compactionMu.Unlock()
	a.nativeContextEditingAccepted.Store(false)
	a.contextEditingRuntimeFallback.Store(false)
	a.compactStuck = false
	a.consecutiveCompacts = 0
	a.lastCompactionTurn.Store(0)
}

// SetSessionPath binds the transcript path used for projection persistence.
func (a *Agent) SetSessionPath(path string) {
	if a == nil {
		return
	}
	a.compactionMu.Lock()
	a.sessionPath = path
	a.compactionMu.Unlock()
}

// SessionPath returns the bound transcript path.
func (a *Agent) SessionPath() string {
	if a == nil {
		return ""
	}
	a.compactionMu.Lock()
	defer a.compactionMu.Unlock()
	return a.sessionPath
}

// SetCacheState records the resume-time cache estimate without rewriting history.
func (a *Agent) SetCacheState(state string) {
	if a == nil {
		return
	}
	switch state {
	case CacheStateWarm, CacheStateCold, CacheStateUnknown:
	default:
		state = CacheStateUnknown
	}
	a.compactionMu.Lock()
	defer a.compactionMu.Unlock()
	a.cacheState = state
	if a.compactionState.SchemaVersion == 0 && len(a.compactionState.Projection.Messages) == 0 {
		a.compactionState.SchemaVersion = compactionStateSchemaCurrent
	}
	a.compactionState.LastCacheState = state
	a.compactionState.UpdatedAt = time.Now().UTC()
}

// CacheState returns the last estimated cache warm/cold/unknown label.
func (a *Agent) CacheState() string {
	if a == nil {
		return CacheStateUnknown
	}
	a.compactionMu.Lock()
	defer a.compactionMu.Unlock()
	if a.cacheState == "" {
		return CacheStateUnknown
	}
	return a.cacheState
}

func (a *Agent) persistCompactionStateLocked() error {
	if a.sessionPath == "" {
		return nil
	}
	return SaveCompactionState(a.sessionPath, a.compactionState)
}

// maintenanceReplacement is the rewrite for one stale tool result. Both the
// planning pass and the re-write that follows archiving go through it, so a
// result cannot be shortened one way while planning and another way on install.
// ok is false when the policy protects the message outright.
func (a *Agent) maintenanceReplacement(m provider.Message, mode toolResultMaintenanceMode, archive string) (string, bool) {
	if a.keepPolicy&KeepErrors != 0 && isErrorMessage(m) {
		// A recorded failure stays recognisable once its text is rewritten, so
		// its passing noise can go. A text-only failure has no such anchor:
		// eliding it would hide that it was ever an error.
		if !failedExecution(m.ToolExecution) {
			return "", false
		}
		return snipFailureResult(m.Content), true
	}
	return rewriteToolResult(m, mode, archive, a.snipStrategyFor(m.Name)), true
}

// applyToolResultMaintenanceView returns a copy of msgs with stale tool results
// snipped or pruned. The canonical transcript is never modified.
func (a *Agent) applyToolResultMaintenanceView(msgs []provider.Message, mode toolResultMaintenanceMode) ([]provider.Message, PruneStats) {
	st := PruneStats{Mode: mode}
	if a.contextWindow <= 0 || len(msgs) == 0 {
		return msgs, st
	}
	st.InputHash = providerVisibleFingerprint(provider.ModelMessages(msgs))
	head, start, ok := a.planCompaction(msgs, 1)
	if !ok {
		if mode != toolResultPrune {
			return msgs, st
		}
		head = 1
		start = len(msgs) - a.recentKeep
		if start < head {
			return msgs, st
		}
	}
	next := append([]provider.Message(nil), msgs...)
	changed := false
	for i := head; i < start; i++ {
		m := next[i]
		if !shouldMaintainToolResult(m, mode) {
			continue
		}
		replacement, ok := a.maintenanceReplacement(m, mode, "projection-view")
		if !ok {
			continue
		}
		if replacement == m.Content {
			continue
		}
		st.SavedChars += len(m.Content) - len(replacement)
		m.Content = replacement
		next[i] = m
		st.Results++
		changed = true
	}
	if !changed {
		return msgs, st
	}
	return next, st
}

// installProjectionIfCurrent closes the compare-and-install window for
// maintenance callers that performed network work from an earlier snapshot.
func (a *Agent) installProjectionIfCurrent(st CompactionState, projectionVersion, generation uint64) error {
	a.compactionMu.Lock()
	defer a.compactionMu.Unlock()
	if a.compactionState.Projection.ProjectionVersion != projectionVersion || a.compactionState.Generation != generation {
		return errCompressStaleContext
	}
	prev := a.compactionState
	a.compactionState = st
	if err := a.persistCompactionStateLocked(); err != nil {
		a.compactionState = prev
		return err
	}
	return nil
}

// promptCacheKey builds a stable lineage key for session + model identity.
// It deliberately excludes message counts, timestamps, and projection hashes.
func promptCacheKey(workspaceID, sessionLineage, modelRef string) string {
	parts := []string{
		strings.TrimSpace(workspaceID),
		strings.TrimSpace(sessionLineage),
		strings.TrimSpace(modelRef),
	}
	return strings.Join(parts, "|")
}
