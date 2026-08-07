package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// ErrCompactionRequired is returned when the prompt exceeds the provider limit
// and compaction could not produce a usable projection. Callers may retry.
var ErrCompactionRequired = errors.New("context exceeds provider limit and compaction failed")

// contextPreflight may compact into a projection under pressure; never rewrites history.
func (a *Agent) contextPreflight(ctx context.Context, trigger string) error {
	if a == nil || a.session == nil || a.contextWindow <= 0 {
		return nil
	}
	msgs, version := a.session.snapshotMessagesVersion()
	cacheKey := a.currentPromptCacheKey()
	est := estimateMessagesTokens(provider.ModelMessages(msgs))
	_, _, high := a.compactThresholds()
	force := max(int(float64(a.contextWindow)*a.compactForceRatio), high)

	// Prefer an existing valid projection when it still covers the pressure.
	if st := a.compactionState; projectionValid(st, msgs, version, cacheKey) {
		visible := modelVisibleFromProjection(st.Projection, msgs)
		projEst := estimateMessagesTokens(provider.ModelMessages(visible))
		if projEst < high || projEst < est {
			return nil
		}
	}

	if est < high {
		return nil
	}

	// Prefer a free prune/snip projection before a paid summarize call.
	pruned, pst := a.applyToolResultMaintenanceView(msgs, toolResultPrune)
	if pst.Results > 0 {
		if err := a.installPruneProjection(pruned, pst); err == nil {
			projEst := estimateMessagesTokens(provider.ModelMessages(pruned))
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(
				"pruned %d stale tool results (~%d tokens est.) before request", pst.Results, est-projEst)})
			if projEst < high {
				return nil
			}
			est = projEst
		}
	}

	forceCompact := est >= force || trigger == CompactionTriggerManual || trigger == CompactionTriggerOverflow
	outcome, err := a.compactToProjection(ctx, trigger, "", forceCompact)
	if err != nil {
		if est >= force {
			return fmt.Errorf("%w", errors.Join(ErrCompactionRequired, err))
		}
		a.sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelInfo,
			Text:   "Context cleanup deferred; continuing with full history.",
			Detail: fmt.Sprintf("compaction failed under soft threshold: %v", err),
		})
		return nil
	}
	// Force + Noop: refuse outside a tool loop; mid-turn is deferred.
	if forceCompact && outcome == CompactionNoop {
		if a.activeTurnStart(msgs) < 0 {
			return fmt.Errorf("%w: no foldable region under force threshold", ErrCompactionRequired)
		}
		a.sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelInfo,
			Text:   "Context cleanup deferred until the current tool turn finishes.",
			Detail: "force threshold reached but the active turn cannot be folded yet",
		})
	}
	return nil
}

// modelVisibleMessages returns the provider-bound message list: a valid
// projection plus any post-projection appends, otherwise the full canonical
// transcript. LocalOnly stripping still happens in prepareSamplingRequest.
func (a *Agent) modelVisibleMessages() []provider.Message {
	if a == nil || a.session == nil {
		return nil
	}
	msgs, version := a.session.snapshotMessagesVersion()
	if st := a.compactionState; projectionValid(st, msgs, version, a.currentPromptCacheKey()) {
		if visible := modelVisibleFromProjection(st.Projection, msgs); len(visible) > 0 {
			return visible
		}
	}
	return msgs
}

// currentPromptCacheKey is the lineage key for the bound session + model.
func (a *Agent) currentPromptCacheKey() string {
	if a == nil {
		return ""
	}
	return promptCacheKey(a.workspaceID, BranchID(a.sessionPath), a.modelRef)
}

// InvalidateProjection drops the in-memory and on-disk projection after
// lineage-changing operations (rewind, branch, fork, system/model change).
func (a *Agent) InvalidateProjection() {
	if a == nil {
		return
	}
	a.compactionState = CompactionState{}
	if a.sessionPath != "" {
		if err := RemoveCompactionState(a.sessionPath); err != nil {
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
	a.sessionPath = sessionPath
	if sessionPath == "" {
		a.compactionState = CompactionState{}
		return
	}
	st, ok, err := LoadCompactionState(sessionPath)
	if err != nil {
		slog.Warn("agent: load context projection", "err", err)
		_ = RemoveCompactionState(sessionPath)
		a.compactionState = CompactionState{}
		return
	}
	if !ok {
		a.compactionState = CompactionState{}
		return
	}
	// Fail closed: known lineage requires an exact stored key (including
	// rejecting blank keys on early sidecars written before this field).
	if key := a.currentPromptCacheKey(); key != "" && st.PromptCacheKey != key {
		a.compactionState = CompactionState{}
		return
	}
	if st.Projection.CoveredPrefixHash == "" {
		a.compactionState = CompactionState{}
		return
	}
	a.compactionState = st
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
	a.sessionPath = path
	a.compactionState = CompactionState{}
	a.cacheState = CacheStateUnknown
}

// SetSessionPath binds the transcript path used for projection persistence.
func (a *Agent) SetSessionPath(path string) {
	if a == nil {
		return
	}
	a.sessionPath = path
}

// SessionPath returns the bound transcript path.
func (a *Agent) SessionPath() string {
	if a == nil {
		return ""
	}
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
	a.cacheState = state
	if a.compactionState.SchemaVersion == 0 && len(a.compactionState.Projection.Messages) == 0 {
		a.compactionState.SchemaVersion = compactionStateSchemaV1
	}
	a.compactionState.LastCacheState = state
	a.compactionState.UpdatedAt = time.Now().UTC()
}

// CacheState returns the last estimated cache warm/cold/unknown label.
func (a *Agent) CacheState() string {
	if a == nil || a.cacheState == "" {
		return CacheStateUnknown
	}
	return a.cacheState
}

// persistCompactionState writes the sidecar when a session path is bound.
func (a *Agent) persistCompactionState() error {
	if a == nil || a.sessionPath == "" {
		return nil
	}
	return SaveCompactionState(a.sessionPath, a.compactionState)
}

// applyToolResultMaintenanceView returns a copy of msgs with stale tool results
// snipped or pruned. The canonical transcript is never modified.
func (a *Agent) applyToolResultMaintenanceView(msgs []provider.Message, mode toolResultMaintenanceMode) ([]provider.Message, PruneStats) {
	var st PruneStats
	if a.contextWindow <= 0 || len(msgs) == 0 {
		return msgs, st
	}
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
		if a.keepPolicy&KeepErrors != 0 && isErrorMessage(m) {
			continue
		}
		replacement := rewriteToolResult(m, mode, "projection-view", a.snipStrategyFor(m.Name))
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

// installProjection replaces the agent's projection and optionally persists it.
// On persist failure the in-memory state is rolled back so half-written state
// cannot be used for subsequent requests.
func (a *Agent) installProjection(st CompactionState) error {
	prev := a.compactionState
	a.compactionState = st
	if err := a.persistCompactionState(); err != nil {
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
