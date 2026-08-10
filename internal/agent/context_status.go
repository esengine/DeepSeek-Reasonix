package agent

import "reasonix/internal/provider"

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
	HardInputCeiling  int
	Headroom          int
	ProjectionVersion uint64
	Blocked           bool
	LastReceipt       *ContextMaintenanceReceipt
}

func (a *Agent) ContextMaintenanceSnapshot() ContextMaintenanceSnapshot {
	if a == nil || a.session == nil {
		return ContextMaintenanceSnapshot{}
	}
	canonical, version := a.session.snapshotMessagesVersion()
	a.compactionMu.Lock()
	state := a.compactionState
	a.compactionMu.Unlock()
	visible := canonical
	if projectionValid(state, canonical, version, a.currentPromptCacheKey()) {
		if projected := modelVisibleFromProjection(state.Projection, canonical); len(projected) > 0 {
			visible = projected
		}
	}
	_, snip, fold := a.compactThresholds()
	snapshot := ContextMaintenanceSnapshot{
		CanonicalTokens:   a.estimatedPromptTokens(provider.ModelMessages(canonical)),
		ProjectedTokens:   a.estimatedPromptTokens(provider.ModelMessages(visible)),
		SnipTrigger:       snip,
		FoldTrigger:       fold,
		ForceTrigger:      a.forceCompactThreshold(fold),
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
	snapshot.Blocked = state.BlockedInputHash != "" && state.BlockedInputHash == currentHash
	if state.LastReceipt != nil {
		receipt := *state.LastReceipt
		snapshot.LastReceipt = &receipt
		if receipt.Status == "applied" && (receipt.Action == "snip" || receipt.Action == "prune" || receipt.Action == "native_tool_clear") {
			snapshot.LastSavedTokens = receipt.SavedTokens
		}
	}
	return snapshot
}
