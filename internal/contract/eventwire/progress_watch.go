package eventwire

import "reasonix/internal/contract/event"

// ProgressWatch is the JSON form of event.ProgressWatch.
type ProgressWatch struct {
	Stalled       bool   `json:"stalled"`
	Cause         string `json:"cause,omitempty"`
	IdleRounds    int    `json:"idleRounds"`
	RoundLimit    int    `json:"roundLimit,omitempty"`
	PromptTokens  int    `json:"promptTokens,omitempty"`
	TokenLimit    int    `json:"tokenLimit,omitempty"`
	TokenMultiple int    `json:"tokenMultiple,omitempty"`
	Pausing       bool   `json:"pausing,omitempty"`
}

func toWireProgressWatch(p *event.ProgressWatch) *ProgressWatch {
	if p == nil {
		return nil
	}
	return &ProgressWatch{
		Stalled: p.Stalled, Cause: p.Cause, IdleRounds: p.IdleRounds, RoundLimit: p.RoundLimit,
		PromptTokens: p.PromptTokens, TokenLimit: p.TokenLimit, TokenMultiple: p.TokenMultiple,
		Pausing: p.Pausing,
	}
}
