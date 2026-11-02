// costquote_wiring.go — where the frontend's sink acquires its cost quotes.
package boot

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/platform/otlp"
	"reasonix/internal/state/stats"
)

// quotedSink wraps the frontend sink so every host consumer — stats recorder,
// CLI metrics, ACP/eventwire bridges, Desktop — reads the same
// occurrence-time quote. Order from the agent:
//
//	Coalesce → GoalUsageTee → Sync → CostQuote → [Recorder] → [OTLP] → frontend
func quotedSink(cfg *config.Config, opts Options, trace *otlp.Exporter) (event.Sink, *event.QuoteContext) {
	ctx := QuoteContextFor(cfg)
	// Innermost is the frontend sink; the recorder sits after quoting so history
	// JSONL stores the CostQuote.
	quoted := otlp.NewSink(opts.Sink, trace, "reasonix")
	if opts.StatsSource.Valid() {
		rec := stats.NewRecorder(quoted, opts.roots().StatsDir(), opts.StatsSource.String())
		if opts.TeamSeat != nil {
			if _, ref := opts.TeamSeat.Team(); ref.Agent != "" {
				rec = rec.ForAgent(ref.Agent)
			}
		}
		quoted = rec
		stats.StartUploadMeter(opts.roots().StatsDir())
	}
	return event.Sync(event.NewCostQuoteSink(quoted, ctx)), ctx
}

// QuoteContextFor is the one construction of the live pricing projection a
// config implies — the same billing mode and catalog-vendor resolution the
// assembled sink uses. It is exported for host-side emitters that price a call
// outside that sink and must not grow a second, drifting copy of these rules.
func QuoteContextFor(cfg *config.Config) *event.QuoteContext {
	return &event.QuoteContext{
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
}
