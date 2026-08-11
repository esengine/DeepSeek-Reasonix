package control

import "fmt"

// GoalRuntimeView is the host-side runtime summary exposed to frontends.
type GoalRuntimeView struct {
	TurnsUsed        int
	TurnsLimit       int
	TokensUsed       int
	RequestsUsed     int
	TokensLimit      int
	NoProgressTurns  int
	NoProgressLimit  int
	LastReason       string
	StopCause        string
	BudgetExtensions int
}

func (g *goalMachine) runtimeView() GoalRuntimeView {
	g.mu.Lock()
	defer g.mu.Unlock()
	last := g.lastEvaluatorReason
	if last == "" {
		last = g.lastContinuationReason
	}
	return GoalRuntimeView{
		TurnsUsed: g.turnsUsed, TurnsLimit: g.turnsLimit,
		TokensUsed: g.tokensUsed, RequestsUsed: g.requestsUsed,
		TokensLimit: 0, NoProgressTurns: g.noProgressTurns,
		NoProgressLimit: g.noProgressLimit, LastReason: last,
		StopCause: g.stopCause, BudgetExtensions: g.budgetExtensions,
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
	return fmt.Sprintf("turns: %d/%d used, tokens: %d, requests: %d, no-progress turns: %d (observational)",
		g.turnsUsed, g.turnsLimit, g.tokensUsed, g.requestsUsed, g.noProgressTurns)
}
