// Where a model's context window comes from. It is a fact about the model, not
// about who serves it, but it was only ever recorded per preset provider — so a
// source added by hand resolved to nothing, and nothing turns compaction off.
package config

// DefaultUnknownContextWindow is the working window for a custom relay whose
// endpoint and model registry provide no capacity metadata. Users can replace
// it with the relay model's actual limit through a model override.
//
// One million tokens, not a smaller "safe" number. The window is what
// compaction is measured against, so a low fallback folds a conversation long
// before the model would have run out of room — and the gateways this applies
// to are the ones serving the large-window models, which is exactly why nobody
// recorded a capacity for them. A relay that really is smaller is the case the
// override exists for, and stating the wrong number in the meantime costs a
// fold nobody needed.
const DefaultUnknownContextWindow = 1_000_000

// ResolvedContextWindow reports the window to run this entry against and
// whether anything established it. Most-specific first: what the config says
// for this provider (including its per-model overrides), then what is known
// about the model itself. Callers that must not confuse "nobody said" with
// "no compaction wanted" need the second return.
func ResolvedContextWindow(e *ProviderEntry) (int, bool) {
	if e == nil {
		return 0, false
	}
	if e.ContextWindow > 0 {
		return e.ContextWindow, true
	}
	if cap, ok := resolvedModelReasoningCapability(e); ok && cap.ContextWindow > 0 {
		return cap.ContextWindow, true
	}
	return 0, false
}

// applyModelCapabilities folds what is known about the model into a resolved
// entry, under everything the configuration said. It runs after
// applyModelOverride for that reason: a per-model override is a declaration and
// wins, this is only the fallback for the fields nobody declared.
func (e *ProviderEntry) applyModelCapabilities() {
	if e == nil || e.ContextWindow > 0 {
		return
	}
	if window, ok := ResolvedContextWindow(e); ok {
		e.ContextWindow = window
		return
	}
	e.ContextWindow = DefaultUnknownContextWindow
}
