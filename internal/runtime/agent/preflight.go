package agent

import (
	"errors"
	"log/slog"
	"reasonix/internal/state/sessionstore"
	"strings"
	"time"

	"reasonix/internal/contract/provider"
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

// ModelVisibleMessages is the view a request carries: the history the host
// would send plus the state it derives for that request. A caller reasoning
// about history alone wants modelVisibleHistory instead.
func (a *Agent) ModelVisibleMessages() []provider.Message { return a.window().modelVisibleMessages() }

// LocalOnly stripping still happens in prepareSamplingRequest.
func (a *contextWindow) modelVisibleMessages() []provider.Message {
	visible := a.withHostContextTail(withAgedToolImages(a.modelVisibleHistory()))
	return a.withTodoIdentityTail(a.withFoldProgressTail(visible, a.foldProgress()))
}

// modelVisibleHistory is the conversation half of that view: frozen body plus
// canonical tail, and nothing derived per request. A fold plans on this, so
// what the host adds at the tail can never become digest input or frozen
// material.
func (a *contextWindow) modelVisibleHistory() []provider.Message {
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
func (a *contextWindow) visibleByFullScan() []provider.Message {
	snap := a.snapshotForProjection()
	msgs := snap.msgs
	a.sess.win.compactionMu.Lock()
	st := a.sess.win.compactionState
	a.sess.win.compactionMu.Unlock()
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
func (a *Agent) ProjectionVersion() uint64 { return a.window().currentProjectionVersion() }

func (a *contextWindow) currentProjectionVersion() uint64 {
	if a == nil {
		return 0
	}
	a.sess.win.compactionMu.Lock()
	defer a.sess.win.compactionMu.Unlock()
	return a.sess.win.compactionState.Projection.ProjectionVersion
}

// currentPromptCacheKey is the lineage key for the bound session + model.
func (a *contextWindow) currentPromptCacheKey() string {
	if a == nil {
		return ""
	}
	a.sess.win.compactionMu.Lock()
	defer a.sess.win.compactionMu.Unlock()
	return a.currentPromptCacheKeyLocked()
}

func (a *contextWindow) currentPromptCacheKeyLocked() string {
	return promptCacheKey(a.workspaceID, sessionstore.BranchID(a.sess.path), a.modelRef)
}

// InvalidateProjection drops the in-memory and on-disk projection after
// lineage-changing operations (rewind, branch, fork, system/model change).
func (a *Agent) InvalidateProjection() {
	if a == nil {
		return
	}
	a.window().invalidateProjection()
}

func (a *contextWindow) invalidateProjection() {
	a.sess.win.compactionMu.Lock()
	path := a.sess.path
	a.sess.win.compactionState = sessionstore.CompactionState{}
	a.sess.win.compactionMu.Unlock()
	a.sess.win.compaction.restart()
	// This build never changes a 1.x log, so 1.x's projection of it stays valid.
	if path != "" && !sessionstore.IsForeignSessionLog(path) {
		if err := sessionstore.RemoveCompactionState(path); err != nil {
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
	a.window().revalidateProjection()
}

func (a *contextWindow) revalidateProjection() {
	a.sess.win.compactionMu.Lock()
	st := a.sess.win.compactionState
	a.sess.win.compactionMu.Unlock()
	if len(st.Projection.Messages) == 0 {
		return
	}
	snap := a.snapshotForProjection()
	if projectionValid(st, snap.msgs, a.currentPromptCacheKey(), snap.fingerprint) {
		return
	}
	a.invalidateProjection()
}

// LoadProjectionSidecar loads the context sidecar into the agent. State it
// cannot read or use is dropped in memory only, so the next request rebuilds
// from canonical and the file stays with whichever release wrote it.
func (a *Agent) LoadProjectionSidecar(sessionPath string) {
	if a == nil {
		return
	}
	a.window().loadProjectionSidecar(sessionPath)
}

func (a *contextWindow) loadProjectionSidecar(sessionPath string) {
	a.sess.win.compactionMu.Lock()
	a.sess.path = sessionPath
	a.sess.win.compactionState = sessionstore.CompactionState{}
	a.sess.win.checkpointState = "none"
	a.sess.win.compactionMu.Unlock()
	if sessionPath == "" {
		a.resetCompactionState()
		return
	}
	st, ok, err := sessionstore.LoadCompactionState(sessionPath)
	if err != nil {
		slog.Warn("agent: load context projection", "err", err,
			"unsupported_schema", errors.Is(err, sessionstore.ErrContextSchemaUnsupported))
		a.resetCompactionState()
		return
	}
	if !ok {
		a.resetCompactionState()
		return
	}
	a.sess.win.compactionMu.Lock()
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
			msgs, _, rewriteVersion = a.sess.conversation.SnapshotWithVersion()
			fingerprint = a.prefixHasher(rewriteVersion)
		}
		if projectionContentValid(st, msgs, fingerprint) {
			normalized, keyOK = key, true
		}
	}
	if (key != "" && !keyOK) || !hasMaintenanceSignal {
		a.sess.win.compactionState = sessionstore.CompactionState{}
		a.sess.win.checkpointState = "none"
		a.sess.win.compactionMu.Unlock()
		return
	}
	// Only rewrite legacy native-editing lineage keys; exact matches and
	// another release line's file stay pure-read.
	needsNormalization := false
	if keyOK && key != "" && normalized != st.PromptCacheKey {
		st.PromptCacheKey = normalized
		needsNormalization = !st.Foreign()
	}
	// Only mark restored when the projection still matches the transcript.
	var msgs []provider.Message
	var fingerprint func([]provider.Message, int) string
	if a.sess.conversation != nil {
		var rewriteVersion int
		msgs, _, rewriteVersion = a.sess.conversation.SnapshotWithVersion()
		fingerprint = a.prefixHasher(rewriteVersion)
	}
	valid := len(st.Projection.Messages) > 0 && projectionValid(st, msgs, key, fingerprint)
	if !valid && len(st.Projection.Messages) > 0 {
		// Keep blocked receipts / telemetry; drop unusable projection body.
		st.Projection = sessionstore.ContextProjection{}
	}
	a.sess.win.compactionState = st
	if valid {
		a.sess.win.checkpointState = "restored"
		if needsNormalization {
			if err := a.persistCompactionStateLocked(); err != nil {
				slog.Warn("agent: persist normalized projection lineage", "err", err)
			}
		}
	} else {
		a.sess.win.checkpointState = "none"
	}
	a.sess.win.compactionMu.Unlock()
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

func (a *contextWindow) resetCompactionState() {
	a.sess.win.compactionMu.Lock()
	a.sess.win.compactionState = sessionstore.CompactionState{}
	a.sess.win.checkpointState = "none"
	a.sess.win.compactionMu.Unlock()
}

// BindSessionPath rebinds projection persistence to path. When loadSidecar is
// true the existing sidecar is loaded (resume/switch); otherwise in-memory
// projection is cleared without deleting another session's sidecar file.
func (a *Agent) BindSessionPath(path string, loadSidecar bool) {
	if a == nil {
		return
	}
	a.window().bindSessionPath(path, loadSidecar)
}

func (a *contextWindow) bindSessionPath(path string, loadSidecar bool) {
	if loadSidecar {
		a.loadProjectionSidecar(path)
		return
	}
	a.sess.win.compactionMu.Lock()
	a.sess.path = path
	a.sess.win.compactionState = sessionstore.CompactionState{}
	a.sess.win.checkpointState = "none"
	a.sess.win.cacheState = CacheStateUnknown
	a.sess.win.compactionMu.Unlock()
	a.sess.win.compaction.restart()
}

// SetSessionPath binds the transcript path used for projection persistence.
func (a *Agent) SetSessionPath(path string) {
	if a == nil {
		return
	}
	a.window().setSessionPath(path)
}

func (a *contextWindow) setSessionPath(path string) {
	a.sess.win.compactionMu.Lock()
	a.sess.path = path
	a.sess.win.compactionMu.Unlock()
}

// SessionPath returns the bound transcript path.
func (a *Agent) SessionPath() string {
	if a == nil {
		return ""
	}
	return a.window().sessionPath()
}

func (a *contextWindow) sessionPath() string {
	a.sess.win.compactionMu.Lock()
	defer a.sess.win.compactionMu.Unlock()
	return a.sess.path
}

// SetCacheState records the resume-time cache estimate without rewriting history.
func (a *Agent) SetCacheState(state string) {
	if a == nil {
		return
	}
	a.window().setCacheState(state)
}

func (a *contextWindow) setCacheState(state string) {
	switch state {
	case CacheStateWarm, CacheStateCold, CacheStateUnknown:
	default:
		state = CacheStateUnknown
	}
	a.sess.win.compactionMu.Lock()
	defer a.sess.win.compactionMu.Unlock()
	a.sess.win.cacheState = state
	if a.sess.win.compactionState.SchemaVersion == 0 && len(a.sess.win.compactionState.Projection.Messages) == 0 {
		a.sess.win.compactionState.SchemaVersion = sessionstore.CompactionStateSchemaCurrent
	}
	a.sess.win.compactionState.LastCacheState = state
	a.sess.win.compactionState.UpdatedAt = time.Now().UTC()
}

// CacheState returns the last estimated cache warm/cold/unknown label.
func (a *Agent) CacheState() string {
	if a == nil {
		return CacheStateUnknown
	}
	return a.window().cacheState()
}

func (a *contextWindow) cacheState() string {
	a.sess.win.compactionMu.Lock()
	defer a.sess.win.compactionMu.Unlock()
	if a.sess.win.cacheState == "" {
		return CacheStateUnknown
	}
	return a.sess.win.cacheState
}

func (a *contextWindow) persistCompactionStateLocked() error {
	if a.sess.path == "" {
		return nil
	}
	return sessionstore.SaveCompactionState(a.sess.path, a.sess.win.compactionState)
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
