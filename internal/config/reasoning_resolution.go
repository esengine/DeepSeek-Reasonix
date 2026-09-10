package config

import "reasonix/internal/provider"

// ResolvedReasoningView is additive UI metadata, resolved by the same adapter
// contract as boot validation. Empty Effective means the server chooses.
type ResolvedReasoningView struct {
	APIFormat string                     `json:"apiFormat"`
	Protocol  string                     `json:"protocol"`
	Selected  string                     `json:"selected"`
	Effective string                     `json:"effective,omitempty"`
	Default   string                     `json:"default,omitempty"`
	Options   []provider.ReasoningOption `json:"options"`
	Error     string                     `json:"error,omitempty"`
}

func ResolveReasoningView(e *ProviderEntry) *ResolvedReasoningView {
	if e == nil {
		return nil
	}
	cap := ReasoningCapabilityForEntry(e)
	effort := EffectiveEffort(e)
	out := &ResolvedReasoningView{APIFormat: e.Kind, Protocol: ReasoningProtocolForEntry(e), Selected: EffortDisplay(e), Effective: effort, Default: cap.Default, Options: cap.Options}
	if err := cap.Validate(e.Model, effort); err != nil {
		out.Error = err.Error()
	}
	if out.Effective == "" {
		out.Effective = cap.Default
	}
	return out
}
