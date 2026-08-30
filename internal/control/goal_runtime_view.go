package control

import "fmt"

// GoalRuntimeView is the host-side runtime summary exposed to frontends.
type GoalRuntimeView struct {
	TurnsUsed        int    `json:"turnsUsed"`
	TurnsLimit       int    `json:"turnsLimit"`
	TokensUsed       int    `json:"tokensUsed"`
	RequestsUsed     int    `json:"requestsUsed,omitempty"`
	WorkDurationMs   int64  `json:"workDurationMs,omitempty"`
	TokensLimit      int    `json:"tokensLimit"`
	NoProgressTurns  int    `json:"noProgressTurns"`
	NoProgressLimit  int    `json:"noProgressLimit"`
	LastReason       string `json:"lastReason,omitempty"`
	StopCause        string `json:"stopCause,omitempty"`
	BudgetExtensions int    `json:"budgetExtensions"`
}

func (g *goalMachine) runtimeView() GoalRuntimeView {
	g.mu.Lock()
	defer g.mu.Unlock()
	last := g.lastEvaluatorReason
	if last == "" {
		last = g.lastContinuationReason
	}
	return GoalRuntimeView{
		TurnsUsed: g.turnsUsed, TurnsLimit: 0,
		TokensUsed: g.tokensUsed, RequestsUsed: g.requestsUsed,
		WorkDurationMs: g.workDurationMs,
		TokensLimit:    g.tokensLimit, NoProgressTurns: g.noProgressTurns,
		NoProgressLimit: 0, LastReason: last,
		StopCause: g.stopCause, BudgetExtensions: 0,
	}
}

func (g *goalMachine) lastContinuationReasonText() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastEvaluatorReason != "" {
		return g.lastEvaluatorReason
	}
	return g.lastContinuationReason
}

func (g *goalMachine) budgetStatusText() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.tokensLimit > 0 {
		return fmt.Sprintf("turns: %d, requests: %d, tokens: %d/%d, work time: %s",
			g.turnsUsed, g.requestsUsed, g.tokensUsed, g.tokensLimit, GoalWorkDurationText(g.workDurationMs))
	}
	return fmt.Sprintf("turns: %d, requests: %d, tokens: %d, work time: %s (observational)",
		g.turnsUsed, g.requestsUsed, g.tokensUsed, GoalWorkDurationText(g.workDurationMs))
}

// GoalWorkDurationText renders cumulative active Goal work time without
// including pauses between Runs.
func GoalWorkDurationText(durationMs int64) string {
	if durationMs <= 0 {
		return "0s"
	}
	totalSeconds := max(int64(1), (durationMs+500)/1000)
	if totalSeconds < 60 {
		return fmt.Sprintf("%ds", totalSeconds)
	}
	totalMinutes := (totalSeconds + 30) / 60
	if totalMinutes < 60 {
		return fmt.Sprintf("%dm", totalMinutes)
	}
	hours, minutes := totalMinutes/60, totalMinutes%60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
