package agent

import "reasonix/internal/contract/provider"

// ContextReport is a point-in-time view of context pressure: the declared
// window, the thresholds derived from it, what the model currently sees, and how
// the last maintenance pass ended. A misconfigured window and a genuinely full
// one produce the same notices, so the numbers behind the decision have to be
// inspectable rather than inferred.
type ContextReport struct {
	Window       int
	HardCeiling  int
	OutputBudget int

	LatestPrompt     int
	CanonicalTokens  int
	ProjectionTokens int
	Projected        bool

	// FoldThreshold is the one automatic trigger. The retired multi-threshold
	// scheme's soft/snip/force levels are gone, not zero-valued.
	FoldThreshold int

	LastTrigger   string
	LastMode      string
	LastSource    int
	LastResult    int
	CacheState    string
	BlockedReason string

	// What the last fold did to the user's own turns. A dropped turn is the
	// loss compaction cannot undo, and the budget that decided it is the
	// user's to set, so both the outcome and the budget are reportable.
	UserTurnsKept        int
	UserTurnsDropped     int
	UserTurnsDroppedToks int
	UserTurnKeepBudget   int
}

// ContextReport samples the current context state. Compaction is disabled when
// Window is zero, in which case the thresholds carry no meaning.
func (a *Agent) ContextReport() ContextReport {
	if a == nil {
		return ContextReport{}
	}
	return a.window().contextReport()
}

func (a *contextWindow) contextReport() ContextReport {
	rep := ContextReport{
		Window:             a.effectiveContextWindow(),
		HardCeiling:        a.hardInputCeiling(),
		OutputBudget:       a.maxOutputTokens,
		CacheState:         a.cacheState(),
		UserTurnKeepBudget: a.keptUserTurnsBudget(),
	}
	retention := a.sess.win.compaction.lastUserTurns
	rep.UserTurnsKept, rep.UserTurnsDropped = retention.Kept, retention.Dropped
	rep.UserTurnsDroppedToks = retention.DroppedTokens
	if u := a.sess.output.lastUsage.Load(); u != nil {
		rep.LatestPrompt = u.LatestPromptTokens()
	}
	if a.sess.conversation != nil {
		canonical, _ := a.sess.conversation.SnapshotMessagesVersion()
		rep.CanonicalTokens = a.estimatedPromptTokens(provider.ModelMessages(canonical))
	}
	visible := a.modelVisibleMessages()
	rep.ProjectionTokens = a.estimatedPromptTokens(provider.ModelMessages(visible))
	rep.Projected = rep.ProjectionTokens != rep.CanonicalTokens

	if a.effectiveContextWindow() > 0 {
		rep.FoldThreshold = a.compactTrigger()
		if _, reason := a.contextMaintenanceBlocked(a.contextMaintenanceInputHash(visible), a.estimatedVisibleRequestTokens(visible), false); reason != "" {
			rep.BlockedReason = reason
		}
	}

	a.sess.win.compactionMu.Lock()
	st := a.sess.win.compactionState
	a.sess.win.compactionMu.Unlock()
	// Prefer LastReceipt; fall back to legacy top-level mirrors from older sidecars.
	if r := st.LastReceipt; r != nil && r.Status == "applied" {
		rep.LastTrigger = r.Trigger
		if r.Action == "summary" {
			rep.LastMode = CompactionModeSummarized
		} else if r.Action != "" {
			rep.LastMode = r.Action
		}
		rep.LastSource, rep.LastResult = r.InputTokens, r.ResultTokens
	} else {
		rep.LastTrigger, rep.LastMode = st.LastTrigger, st.LastMode
		rep.LastSource, rep.LastResult = st.LastSourceTokens, st.LastResultTokens
	}
	if rep.BlockedReason == "" {
		if r := st.LastReceipt; r != nil && (r.Status == "blocked" || r.Status == "failed") {
			rep.BlockedReason = r.Reason
		} else {
			rep.BlockedReason = st.BlockedReason
		}
	}
	return rep
}
