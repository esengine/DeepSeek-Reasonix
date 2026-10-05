// costquote_wiring.go — where the frontend's sink acquires its cost quotes.
package boot

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/state/stats"
)

// quotedSink wraps the frontend sink so every host consumer — stats recorder,
// CLI metrics, ACP/eventwire bridges, Desktop — reads the same
// occurrence-time quote. Order from the agent:
//
//	Coalesce → GoalUsageTee → Sync → CostQuote → [Recorder] → frontend
func quotedSink(cfg *config.Config, opts Options) event.Sink {
	ctx := &event.QuoteContext{
		DisplayRequest: pricing.DisplayRequest{
			Currency: cfg.ExplicitDisplayCurrency(),
			Source:   pricing.DisplaySourceExplicit,
		},
		BillingModeForModel: func(modelRef string) string {
			entry, ok := cfg.ResolveModel(modelRef)
			if !ok {
				return ""
			}
			return entry.ProviderBillingMode()
		},
		// The vendor whose table applies is read off the endpoint's host, never
		// off what the entry is named — a relay serving the same model bills on
		// its own terms.
		CatalogProviderForModel: cfg.OfficialCatalogProvider,
	}
	// Innermost is the frontend sink; the recorder sits after quoting so history
	// JSONL stores the CostQuote.
	quoted := opts.Sink
	if opts.StatsSource.Valid() {
		quoted = stats.NewRecorder(quoted, opts.roots().StatsDir(), opts.StatsSource.String())
	}
	return event.Sync(event.NewCostQuoteSink(quoted, ctx))
}
