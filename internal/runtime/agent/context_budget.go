package agent

import "reasonix/internal/contract/tool"

// ContextBudget reports the room left before the next automatic fold, measured
// with the estimate the compaction thresholds themselves compare against so
// what the model is told and what the host acts on cannot drift apart.
func (a *Agent) ContextBudget() tool.ContextBudget {
	if a == nil {
		return unmeasuredContextBudget("no active agent session")
	}
	return a.window().contextBudget()
}

func (a *contextWindow) contextBudget() tool.ContextBudget {
	trigger := a.compactTrigger()
	window := a.effectiveContextWindow()
	if trigger <= 0 || window <= 0 {
		return unmeasuredContextBudget("the active provider declares no context window, so compaction is disabled")
	}
	used := a.contextUsedTokens()
	return tool.ContextBudget{
		Status:          "ok",
		TokensRemaining: max(0, trigger-used),
		TokensUsed:      used,
		CompactAt:       trigger,
		Window:          window,
	}
}

func unmeasuredContextBudget(reason string) tool.ContextBudget {
	return tool.ContextBudget{Status: "unmeasured", Reason: reason}
}
