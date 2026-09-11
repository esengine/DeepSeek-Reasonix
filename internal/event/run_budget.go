package event

import "reasonix/internal/nilutil"

// RunBudgetTotals is one scope's accumulated spend: counts and money, never
// content. Priced is false when no price table covered it, so a reader can
// tell a genuinely free stretch from an unpriced one.
type RunBudgetTotals struct {
	Rounds       int     `json:"rounds"`
	Requests     int     `json:"requests"`
	PromptTokens int     `json:"promptTokens"`
	OutputTokens int     `json:"outputTokens"`
	Cost         float64 `json:"cost"`
	Priced       bool    `json:"priced"`
	ElapsedMs    int64   `json:"elapsedMs"`
}

// RunBudgetSample reports both scopes after a round. Turn is the current Run;
// Task spans every Run continuing the same work, because "continue" starts a
// new Run and the spend worth stopping accrues across them.
type RunBudgetSample struct {
	Turn     RunBudgetTotals `json:"turn"`
	Task     RunBudgetTotals `json:"task"`
	Currency string          `json:"currency,omitempty"`
}

// RunBudgetSink is an optional sink capability for the per-round spend axis.
// Shadow means observed, not enforced: no threshold reads these yet.
type RunBudgetSink interface {
	RecordRunBudget(RunBudgetSample)
}

// RecordRunBudget forwards a turn's spend reading only to sinks that opt in.
// Ordinary UI sinks receive nothing.
func RecordRunBudget(s Sink, sample RunBudgetSample) {
	if nilutil.IsNil(s) {
		return
	}
	if rb, ok := s.(RunBudgetSink); ok {
		rb.RecordRunBudget(sample)
	}
}
