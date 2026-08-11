package event

import "reasonix/internal/nilutil"

// RunBudgetSample is one turn's accumulated spend after a round: counts and
// money, never content. Priced is false when no price table covered the turn,
// so a reader can tell a genuinely free turn from an unpriced one.
type RunBudgetSample struct {
	Rounds       int     `json:"rounds"`
	Requests     int     `json:"requests"`
	PromptTokens int     `json:"promptTokens"`
	OutputTokens int     `json:"outputTokens"`
	Cost         float64 `json:"cost"`
	Currency     string  `json:"currency,omitempty"`
	Priced       bool    `json:"priced"`
	ElapsedMs    int64   `json:"elapsedMs"`
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
