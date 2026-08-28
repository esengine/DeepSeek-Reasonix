package boot

import "reasonix/internal/config"

// learnedWindowOptions derives the persisted provider observation for an entry
// and the callback that records newer downward observations. Explicit windows
// opt out entirely; default/zero windows may learn.
func learnedWindowOptions(store *config.LearnedWindowStore, e *config.ProviderEntry) (window, completion int, persist func(int, int)) {
	if store == nil || e == nil || config.EffectiveContextWindowSource(e) == config.ContextWindowSourceExplicit {
		return 0, 0, nil
	}
	lw := store.Get(e.BaseURL, e.Model)
	return lw.WindowTokens, lw.CompletionBudget, func(window, completion int) {
		store.Update(e.BaseURL, e.Model, config.LearnedWindow{WindowTokens: window, CompletionBudget: completion})
	}
}
