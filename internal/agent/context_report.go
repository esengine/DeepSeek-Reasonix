package agent

import "reasonix/internal/provider"

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

	SoftThreshold  int
	SnipThreshold  int
	FoldThreshold  int
	ForceThreshold int

	LastTrigger   string
	LastMode      string
	LastSource    int
	LastResult    int
	CacheState    string
	BlockedReason string
}

// ContextReport samples the current context state. Compaction is disabled when
// Window is zero, in which case the thresholds carry no meaning.
func (a *Agent) ContextReport() ContextReport {
	if a == nil {
		return ContextReport{}
	}
	rep := ContextReport{
		Window:       a.contextWindow,
		HardCeiling:  a.hardInputCeiling(),
		OutputBudget: a.maxOutputTokens,
		CacheState:   a.CacheState(),
	}
	if u := a.lastUsage.Load(); u != nil {
		rep.LatestPrompt = u.LatestPromptTokens()
	}
	if a.session != nil {
		canonical, _ := a.session.snapshotMessagesVersion()
		rep.CanonicalTokens = a.estimatedPromptTokens(provider.ModelMessages(canonical))
	}
	visible := a.modelVisibleMessages()
	rep.ProjectionTokens = a.estimatedPromptTokens(provider.ModelMessages(visible))
	rep.Projected = rep.ProjectionTokens != rep.CanonicalTokens

	if a.contextWindow > 0 {
		rep.FoldThreshold = a.compactTrigger()
		if _, reason := a.contextMaintenanceBlocked(a.contextMaintenanceInputHash(visible)); reason != "" {
			rep.BlockedReason = reason
		}
	}

	a.compactionMu.Lock()
	st := a.compactionState
	a.compactionMu.Unlock()
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
