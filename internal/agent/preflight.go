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
	return promptCacheKey(a.workspaceID, BranchID(a.sessionPath), a.modelRef)
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
	a.compaction.stuck = false
	a.compaction.consecutive = 0
	a.compaction.lastTurn.Store(0)
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
	a.compactionMu.Lock()
	a.sessionPath = sessionPath
	a.compactionState = CompactionState{}
	a.checkpointState = "none"
	a.compactionMu.Unlock()
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
	key := a.currentPromptCacheKeyLocked()
	normalized, keyOK := lineageKeyCompatible(st.PromptCacheKey, key)
	// Keep receipt-only blocked/failed sidecars (no projection body) and legacy
	// top-level BlockedInputHash so generation-scoped suppressions survive restart.
	hasMaintenanceSignal := st.Projection.CoveredPrefixHash != "" ||
		st.BlockedInputHash != "" ||
		(st.LastReceipt != nil && (st.LastReceipt.Status == "blocked" || st.LastReceipt.Status == "failed" ||
			st.LastReceipt.Status == "applied"))
	if (key != "" && !keyOK) || !hasMaintenanceSignal {
		a.compactionState = CompactionState{}
		a.checkpointState = "none"
		a.compactionMu.Unlock()
		return
	}
	// Only rewrite legacy native-editing lineage keys; exact matches stay pure-read.
	needsNormalization := false
	if keyOK && key != "" && normalized != st.PromptCacheKey {
		st.PromptCacheKey = normalized
		needsNormalization = true
	}
	// Only mark restored when the projection still matches the transcript.
	var msgs []provider.Message
	var version uint64
	if a.session != nil {
		msgs, version = a.session.snapshotMessagesVersion()
	}
	valid := len(st.Projection.Messages) > 0 && projectionValid(st, msgs, version, key)
	if !valid && len(st.Projection.Messages) > 0 {
		// Keep blocked receipts / telemetry; drop unusable projection body.
		st.Projection = ContextProjection{}
	}
	a.compactionState = st
	if valid {
		a.checkpointState = "restored"
		if needsNormalization {
			if err := a.persistCompactionStateLocked(); err != nil {
				slog.Warn("agent: persist normalized projection lineage", "err", err)
			}
		}
	} else {
		a.checkpointState = "none"
	}
	a.compactionMu.Unlock()
}

// lineageKeyCompatible reports whether a stored PromptCacheKey still belongs to
// the current session/model lineage. Legacy native context-editing keys used a
// "|context-editing-native-..." suffix on an otherwise matching base key.
func lineageKeyCompatible(stored, current string) (normalized string, ok bool) {
	stored, current = strings.TrimSpace(stored), strings.TrimSpace(current)
	if current == "" {
		// Unknown current lineage: accept any stored key as-is.
		return stored, true
	}
	if stored == "" {
		return "", false
	}
	if stored == current {
		return current, true
	}
	const nativeSuffix = "|context-editing-native"
	if strings.HasPrefix(stored, current+nativeSuffix) {
		return current, true
	}
	if i := strings.Index(stored, nativeSuffix); i > 0 && stored[:i] == current {
		return current, true
	}
	return "", false
}

func (a *Agent) resetCompactionState() {
	a.compactionMu.Lock()
	a.compactionState = CompactionState{}
	a.checkpointState = "none"
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
	a.compactionMu.Lock()
	a.sessionPath = path
	a.compactionState = CompactionState{}
	a.checkpointState = "none"
	a.cacheState = CacheStateUnknown
	a.compactionMu.Unlock()
	a.compaction.stuck = false
	a.compaction.consecutive = 0
	a.compaction.lastTurn.Store(0)
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
