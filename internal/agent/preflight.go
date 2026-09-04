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

// IsCompactionDeclined reports a fold the kernel judged not worth installing —
// the candidate was no smaller, or nothing foldable remained. It is a verdict,
// not a failure, and a caller should say so rather than report an error.
func IsCompactionDeclined(err error) bool {
	return errors.Is(err, errCheckpointRejected)
}

// CompactionDeclineReason is the verdict without the sentinel's own prefix, so
// a frontend can say why in its own sentence instead of quoting an error.
func CompactionDeclineReason(err error) string {
	if err == nil {
		return ""
	}
	var rejected *checkpointRejection
	if errors.As(err, &rejected) {
		return rejected.reason
	}
	return err.Error()
}

// modelVisibleMessages returns the provider-bound message list: a valid
// projection plus any post-projection appends, otherwise the full canonical
// transcript. LocalOnly stripping still happens in prepareSamplingRequest.
func (a *Agent) modelVisibleMessages() []provider.Message {
	if a == nil || a.sess.conversation == nil {
		return nil
	}
	if visible, ok := a.visibleBehindMemoisedFold(); ok {
		return visible
	}
	return a.visibleByFullScan()
}

// visibleByFullScan is the same answer read out of the canonical transcript. It
// runs where the memo cannot vouch for the folded prefix — a fold, a rewrite,
// the first turn after a load — and refills the memo on its way through.
func (a *Agent) visibleByFullScan() []provider.Message {
	snap := a.snapshotForProjection()
	msgs := snap.msgs
	a.sess.compactionMu.Lock()
	st := a.sess.compactionState
	a.sess.compactionMu.Unlock()
	if projectionValid(st, msgs, a.currentPromptCacheKey(), snap.fingerprint) {
		if visible := modelVisibleFromProjection(st.Projection, msgs); len(visible) > 0 {
			return visible
		}
	}
	return msgs
}

// ProjectionVersion is the installed fold's version. A caller comparing folds
// across a run wants this scalar, not ContextMaintenanceSnapshot — that one
// reads the whole canonical transcript to report what the fold saved, which is
// a large answer to a small question.
func (a *Agent) ProjectionVersion() uint64 { return a.currentProjectionVersion() }

func (a *Agent) currentProjectionVersion() uint64 {
	if a == nil {
		return 0
	}
	a.sess.compactionMu.Lock()
	defer a.sess.compactionMu.Unlock()
	return a.sess.compactionState.Projection.ProjectionVersion
}

// currentPromptCacheKey is the lineage key for the bound session + model.
func (a *Agent) currentPromptCacheKey() string {
	if a == nil {
		return ""
	}
	a.sess.compactionMu.Lock()
	defer a.sess.compactionMu.Unlock()
	return a.currentPromptCacheKeyLocked()
}

func (a *Agent) currentPromptCacheKeyLocked() string {
	return promptCacheKey(a.workspaceID, BranchID(a.sess.path), a.modelRef)
}

// InvalidateProjection drops the in-memory and on-disk projection after
// lineage-changing operations (rewind, branch, fork, system/model change).
func (a *Agent) InvalidateProjection() {
	if a == nil {
		return
	}
	a.sess.compactionMu.Lock()
	path := a.sess.path
	a.sess.compactionState = CompactionState{}
	a.sess.compactionMu.Unlock()
	a.sess.compaction.restart()
	if path != "" {
		if err := RemoveCompactionState(path); err != nil {
			slog.Warn("agent: remove context projection", "err", err)
		}
	}
}

// RevalidateProjection drops the installed projection only when it no longer
// covers the current canonical transcript. A caller that rewrote history asks
// this instead of deciding for itself: coverage is this subsystem's judgement,
// and a second copy of it in a caller is how two contracts start.
func (a *Agent) RevalidateProjection() {
	if a == nil {
		return
	}
	a.sess.compactionMu.Lock()
	st := a.sess.compactionState
	a.sess.compactionMu.Unlock()
	if len(st.Projection.Messages) == 0 {
		return
	}
	snap := a.snapshotForProjection()
	if projectionValid(st, snap.msgs, a.currentPromptCacheKey(), snap.fingerprint) {
		return
	}
	a.InvalidateProjection()
}

// LoadProjectionSidecar loads the context sidecar into the agent. Corrupt or
// incompatible state is dropped so the next request rebuilds from canonical.
// Sidecars whose PromptCacheKey does not match the current agent lineage are
// discarded without deleting the file (another model may still own it).
func (a *Agent) LoadProjectionSidecar(sessionPath string) {
	if a == nil {
		return
	}
	a.sess.compactionMu.Lock()
	a.sess.path = sessionPath
	a.sess.compactionState = CompactionState{}
	a.sess.checkpointState = "none"
	a.sess.compactionMu.Unlock()
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
	a.sess.compactionMu.Lock()
	key := a.currentPromptCacheKeyLocked()
	normalized, keyOK := lineageKeyCompatible(st.PromptCacheKey, key)
	// Keep receipt-only blocked/failed sidecars (no projection body) and legacy
	// top-level BlockedInputHash so generation-scoped suppressions survive restart.
	hasMaintenanceSignal := st.Projection.CoveredPrefixHash != "" ||
		st.BlockedInputHash != "" ||
		(st.LastReceipt != nil && (st.LastReceipt.Status == "blocked" || st.LastReceipt.Status == "failed" ||
			st.LastReceipt.Status == "applied"))
	if key != "" && !keyOK {
		// Lineage key changed (upgrade, model/workspace switch). Rebind when
		// the projection body still matches the canonical covered prefix.
		var msgs []provider.Message
		var fingerprint func([]provider.Message, int) string
		if a.sess.conversation != nil {
			var rewriteVersion int
			msgs, _, rewriteVersion = a.sess.conversation.snapshotWithVersion()
			fingerprint = a.prefixHasher(rewriteVersion)
		}
		if projectionContentValid(st, msgs, fingerprint) {
			normalized, keyOK = key, true
		}
	}
	if (key != "" && !keyOK) || !hasMaintenanceSignal {
		a.sess.compactionState = CompactionState{}
		a.sess.checkpointState = "none"
		a.sess.compactionMu.Unlock()
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
	var fingerprint func([]provider.Message, int) string
	if a.sess.conversation != nil {
		var rewriteVersion int
		msgs, _, rewriteVersion = a.sess.conversation.snapshotWithVersion()
		fingerprint = a.prefixHasher(rewriteVersion)
	}
	valid := len(st.Projection.Messages) > 0 && projectionValid(st, msgs, key, fingerprint)
	if !valid && len(st.Projection.Messages) > 0 {
		// Keep blocked receipts / telemetry; drop unusable projection body.
		st.Projection = ContextProjection{}
	}
	a.sess.compactionState = st
	if valid {
		a.sess.checkpointState = "restored"
		if needsNormalization {
			if err := a.persistCompactionStateLocked(); err != nil {
				slog.Warn("agent: persist normalized projection lineage", "err", err)
			}
		}
	} else {
		a.sess.checkpointState = "none"
	}
	a.sess.compactionMu.Unlock()
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
	a.sess.compactionMu.Lock()
	a.sess.compactionState = CompactionState{}
	a.sess.checkpointState = "none"
	a.sess.compactionMu.Unlock()
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
	a.sess.compactionMu.Lock()
	a.sess.path = path
	a.sess.compactionState = CompactionState{}
	a.sess.checkpointState = "none"
	a.sess.cacheState = CacheStateUnknown
	a.sess.compactionMu.Unlock()
	a.sess.compaction.restart()
}

// SetSessionPath binds the transcript path used for projection persistence.
func (a *Agent) SetSessionPath(path string) {
	if a == nil {
		return
	}
	a.sess.compactionMu.Lock()
	a.sess.path = path
	a.sess.compactionMu.Unlock()
}

// SessionPath returns the bound transcript path.
func (a *Agent) SessionPath() string {
	if a == nil {
		return ""
	}
	a.sess.compactionMu.Lock()
	defer a.sess.compactionMu.Unlock()
	return a.sess.path
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
	a.sess.compactionMu.Lock()
	defer a.sess.compactionMu.Unlock()
	a.sess.cacheState = state
	if a.sess.compactionState.SchemaVersion == 0 && len(a.sess.compactionState.Projection.Messages) == 0 {
		a.sess.compactionState.SchemaVersion = compactionStateSchemaCurrent
	}
	a.sess.compactionState.LastCacheState = state
	a.sess.compactionState.UpdatedAt = time.Now().UTC()
}

// CacheState returns the last estimated cache warm/cold/unknown label.
func (a *Agent) CacheState() string {
	if a == nil {
		return CacheStateUnknown
	}
	a.sess.compactionMu.Lock()
	defer a.sess.compactionMu.Unlock()
	if a.sess.cacheState == "" {
		return CacheStateUnknown
	}
	return a.sess.cacheState
}

func (a *Agent) persistCompactionStateLocked() error {
	if a.sess.path == "" {
		return nil
	}
	return SaveCompactionState(a.sess.path, a.sess.compactionState)
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
