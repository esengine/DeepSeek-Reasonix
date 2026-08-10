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
		rep.CanonicalTokens = estimateMessagesTokens(provider.ModelMessages(canonical))
	}
	visible := a.modelVisibleMessages()
	rep.ProjectionTokens = estimateMessagesTokens(provider.ModelMessages(visible))
	rep.Projected = rep.ProjectionTokens != rep.CanonicalTokens

	if a.contextWindow > 0 {
		soft, snip, fold := a.compactThresholds()
		rep.SoftThreshold, rep.SnipThreshold, rep.FoldThreshold = soft, snip, fold
		rep.ForceThreshold = a.forceCompactThreshold(fold)
		if _, reason := a.contextMaintenanceBlocked(a.contextMaintenanceInputHash(visible)); reason != "" {
			rep.BlockedReason = reason
		}
	}

	a.compactionMu.Lock()
	st := a.compactionState
	a.compactionMu.Unlock()
	rep.LastTrigger, rep.LastMode = st.LastTrigger, st.LastMode
	rep.LastSource, rep.LastResult = st.LastSourceTokens, st.LastResultTokens
	if rep.BlockedReason == "" {
		rep.BlockedReason = st.BlockedReason
	}
	return rep
}
