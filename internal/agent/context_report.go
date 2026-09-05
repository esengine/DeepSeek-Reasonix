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

	// Breakdown classifies the current model-visible view so maintainers can
	// see who consumes the window before tuning any governance. ChatTokens
	// includes the (pinned) system message; SchemaTokens is the per-request
	// tool-schema mass that compaction can never reclaim.
	//
	// Units: ToolResultTokens and ChatTokens are measured by the planning
	// estimator (estimateMessagesTokens) — the "for internal planning budgets
	// only; against the window it would compact 4x early" unit documented on
	// estimatedPromptTokens in output_budget.go. It reads up to ~4x high next
	// to the window-denominated fields (ProjectionTokens, Window,
	// FoldThreshold), so never sum these into or compare them against those;
	// only ratios between the two message buckets carry meaning. SchemaTokens
	// is in a third unit, the request estimator (estimatedRequestTokens),
	// which session calibration re-scales even though the message buckets
	// stay deterministic — it is not comparable with either bucket either.
	ToolResultTokens int // planning tokens (estimateMessagesTokens): deterministic, calibration-free
	ChatTokens       int // planning tokens (estimateMessagesTokens): deterministic, calibration-free
	SchemaTokens     int // request tokens (estimatedRequestTokens): re-scaled by session calibration

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
	if u := a.sess.output.lastUsage.Load(); u != nil {
		rep.LatestPrompt = u.LatestPromptTokens()
	}
	if a.sess.conversation != nil {
		canonical, _ := a.sess.conversation.snapshotMessagesVersion()
		rep.CanonicalTokens = a.estimatedPromptTokens(provider.ModelMessages(canonical))
	}
	visible := a.modelVisibleMessages()
	rep.ProjectionTokens = a.estimatedPromptTokens(provider.ModelMessages(visible))
	rep.Projected = rep.ProjectionTokens != rep.CanonicalTokens
	for _, m := range visible {
		if m.LocalOnly {
			continue
		}
		if m.Role == provider.RoleTool {
			rep.ToolResultTokens += estimateMessagesTokens([]provider.Message{m})
			continue
		}
		rep.ChatTokens += estimateMessagesTokens([]provider.Message{m})
	}
	if schemas := a.providerToolSchemas(); len(schemas) > 0 {
		rep.SchemaTokens = a.estimatedRequestTokens(provider.Request{Tools: schemas})
	}

	if a.contextWindow > 0 {
		rep.FoldThreshold = a.compactTrigger()
		if _, reason := a.contextMaintenanceBlocked(a.contextMaintenanceInputHash(visible)); reason != "" {
			rep.BlockedReason = reason
		}
	}

	a.sess.compactionMu.Lock()
	st := a.sess.compactionState
	a.sess.compactionMu.Unlock()
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
