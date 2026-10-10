package agent

import (
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// ContextMaintenanceSnapshot is a read-only view of the current provider-bound
// context. It separates present composition from cumulative summary-call cost.
type ContextMaintenanceSnapshot struct {
	CanonicalTokens   int
	ProjectedTokens   int
	SummaryTokens     int
	LastSavedTokens   int
	SnipTrigger       int
	FoldTrigger       int
	ForceTrigger      int
	TriggerTokens     int
	CheckpointState   string
	HardInputCeiling  int
	Headroom          int
	ProjectionVersion uint64
	Blocked           bool
	LastReceipt       *sessionstore.ContextMaintenanceReceipt
}

func (a *Agent) ContextMaintenanceSnapshot() ContextMaintenanceSnapshot {
	return a.window().contextMaintenanceSnapshot()
}

func (a *contextWindow) contextMaintenanceSnapshot() ContextMaintenanceSnapshot {
	if a == nil || a.sess.conversation == nil {
		return ContextMaintenanceSnapshot{}
	}
	snap := a.snapshotForProjection()
	canonical := snap.msgs
	a.sess.win.compactionMu.Lock()
	state := a.sess.win.compactionState
	checkpointState := a.sess.win.checkpointState
	a.sess.win.compactionMu.Unlock()
	visible := canonical
	valid := projectionValid(state, canonical, a.currentPromptCacheKey(), snap.fingerprint)
	if valid {
		if projected := modelVisibleFromProjection(state.Projection, canonical); len(projected) > 0 {
			visible = projected
		}
	}
	trigger := a.compactTrigger()
	// UI checkpoint label requires a still-valid covered prefix, not merely
	// that the sidecar loaded.
	uiCheckpoint := "none"
	if valid && len(state.Projection.Messages) > 0 {
		uiCheckpoint = stateCheckpointState(checkpointState, state)
	}
	snapshot := ContextMaintenanceSnapshot{
		CanonicalTokens:   a.estimatedVisibleRequestTokens(canonical),
		ProjectedTokens:   a.estimatedVisibleRequestTokens(visible),
		FoldTrigger:       trigger,
		TriggerTokens:     trigger,
		CheckpointState:   uiCheckpoint,
		HardInputCeiling:  a.hardInputCeiling(),
		ProjectionVersion: state.Projection.ProjectionVersion,
	}
	for _, msg := range visible {
		if isCompactionSummary(msg) {
			snapshot.SummaryTokens += a.estimatedPromptTokens([]provider.Message{msg})
		}
	}
	snapshot.Headroom = max(0, snapshot.HardInputCeiling-snapshot.ProjectedTokens)
	currentHash := a.contextMaintenanceInputHash(visible)
	if state.LastReceipt != nil {
		receipt := *state.LastReceipt
		snapshot.LastReceipt = &receipt
		if receipt.Status == "applied" && (receipt.Action == "prune" || receipt.Action == "summary") {
			snapshot.LastSavedTokens = receipt.SavedTokens
		}
		// Generation-scoped blocked/failed receipts match contextMaintenanceBlocked.
		if a.blockedReceiptHolds(&receipt, snapshot.ProjectedTokens) {
			snapshot.Blocked = true
		}
	}
	// Legacy sidecars may only have top-level BlockedInputHash; a receipt, when
	// present, is the sole authority, as in contextMaintenanceBlocked.
	if state.LastReceipt == nil && state.BlockedInputHash != "" && state.BlockedInputHash == currentHash {
		snapshot.Blocked = true
	}
	return snapshot
}

func stateCheckpointState(runtimeState string, state sessionstore.CompactionState) string {
	if len(state.Projection.Messages) == 0 {
		return "none"
	}
	if runtimeState == "applied" {
		return "applied"
	}
	return "restored"
}
