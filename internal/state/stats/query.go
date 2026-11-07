package stats

import (
	"context"
	"fmt"
	"maps"
	"reasonix/internal/contract/pricing"
	"sort"
	"strings"
	"time"

	"reasonix/internal/state/usagecatalog"
)

// DailyTokens is one day's token usage and turn count in a trend series.
type DailyTokens struct {
	Day        string           `json:"day"` // "2026-08-02"
	Total      int              `json:"total"`
	ByModel    map[string]int64 `json:"byModel"`        // model ref -> tokens
	ByProvider map[string]int64 `json:"byProvider"`     // provider name -> tokens
	Requests   int              `json:"requests"`       // provider API requests
	Turns      int              `json:"turns"`          // completed turns
	CacheHit   int64            `json:"cacheHit"`       // cached input tokens that day
	CacheMiss  int64            `json:"cacheMiss"`      // uncached input tokens that day
	Cost       []pricing.Money  `json:"cost,omitempty"` // billed, one entry per billing currency
	// EstimateCost is what a plan's API-priced estimates add up to; it never
	// shares a figure with Cost.
	EstimateCost []pricing.Money `json:"estimateCost,omitempty"`
}

// HourlyTokens is one hour's usage in a single-day range. Hour is "09:00".
type HourlyTokens struct {
	Hour         string           `json:"hour"`
	Total        int              `json:"total"`
	ByModel      map[string]int64 `json:"byModel"`
	ByProvider   map[string]int64 `json:"byProvider"`
	Requests     int              `json:"requests"`
	Turns        int              `json:"turns"`
	CacheHit     int64            `json:"cacheHit"`
	CacheMiss    int64            `json:"cacheMiss"`
	Cost         []pricing.Money  `json:"cost,omitempty"`
	EstimateCost []pricing.Money  `json:"estimateCost,omitempty"`
}

// ModelUsage is one model's aggregate within the range.
type ModelUsage struct {
	Model    string  `json:"model"`
	Provider string  `json:"provider"`
	Tokens   int64   `json:"tokens"`
	Percent  float64 `json:"percent"` // 0..100
}

// ProviderUsage is one provider's aggregate within the range (each provider
// may serve several models).
type ProviderUsage struct {
	Provider string  `json:"provider"`
	Tokens   int64   `json:"tokens"`
	Percent  float64 `json:"percent"`
}

// RangeStats is the full aggregate the settings panel renders for one time
// range and source filter.
type RangeStats struct {
	From string `json:"from"` // inclusive
	To   string `json:"to"`   // inclusive
	// Totals
	Tokens    int64 `json:"tokens"`
	Requests  int   `json:"requests"` // provider API requests
	Turns     int   `json:"turns"`    // completed turns
	CacheHit  int64 `json:"cache_hit"`
	CacheMiss int64 `json:"cache_miss"`
	// Derived
	// Cost is one entry per billing currency: summing two would have to invent
	// an exchange rate, and the rate a turn was billed at is not today's.
	Cost []pricing.Money `json:"cost,omitempty"`
	// EstimateCost is the API-priced estimate of what subscription-equivalent
	// connections used, one entry per currency. It is not money paid and is kept
	// out of Cost, so a plan's estimate never reads as a charge.
	EstimateCost []pricing.Money `json:"estimateCost,omitempty"`
	// CostEstimated marks a total that folded in at least one quote priced from
	// a fallback rate card. A surface labelling the sum "paid" has to say so.
	CostEstimated bool `json:"costEstimated,omitempty"`
	// CostIncomplete marks a total that counted usage for at least one call
	// whose money is unknown. Cost is then a floor, not the whole bill.
	CostIncomplete bool `json:"costIncomplete,omitempty"`
	// UnpricedCalls is how many of the counted calls are under that floor.
	UnpricedCalls int `json:"unpricedCalls,omitempty"`
	// IncompleteReasons names why those calls were unpriced, sorted and
	// deduplicated; a row with no recorded reason still counts above.
	IncompleteReasons []string `json:"incompleteReasons,omitempty"`
	// CountedCalls is the denominator UnpricedCalls is a subset of: one per
	// non-turn row that carried tokens, a price, or a quote that could not
	// price the call. Turn markers and request-only failures are not calls.
	CountedCalls int `json:"countedCalls"`
	// EstimatedCalls is how many priced calls were quoted from an estimated or
	// fallback rate card rather than a provider-reported amount.
	EstimatedCalls int `json:"estimatedCalls"`
	// UnnamedCardCalls is how many counted calls recorded a price but no
	// pricing_fingerprint: the amount is known, the rate card behind it is not.
	UnnamedCardCalls int `json:"unnamedCardCalls"`
	// UnclassifiedCardCalls is how many counted calls recorded no price fact at
	// all, so whether their card was named was never recorded. A non-zero value
	// makes UnnamedCardCalls unknowable for the range, not zero.
	UnclassifiedCardCalls int    `json:"unclassifiedCardCalls"`
	ActiveDays            int    `json:"active_days"`
	TopModel              string `json:"top_model"`
	TopProvider           string `json:"top_provider"`
	// Series
	Daily []DailyTokens `json:"daily"`
	// Hourly is filled only when the range is one calendar day; the day
	// rollup cannot resolve an hour.
	Hourly    []HourlyTokens  `json:"hourly,omitempty"`
	Models    []ModelUsage    `json:"models"`
	Providers []ProviderUsage `json:"providers"`
}

// SourceFilter selects which source labels to aggregate; "" or "all" includes
// every source.
type SourceFilter struct {
	Source string
	From   time.Time
	To     time.Time
	// Currency is the display currency costs are read in; empty reads each
	// turn in the currency it was billed.
	Currency string
}

// Query aggregates the daily stats files intersecting [from, to]. Missing days
// yield zero entries. When SourceFilter.Source is set, only records whose
// Source matches are counted.
func (w *Writer) Query(f SourceFilter) (RangeStats, error) {
	// The catalog rolls up by day, so a single-day question has to read the
	// authoritative records for its hour buckets.
	if f.From.Format(dayLayout) == f.To.Format(dayLayout) {
		return w.querySingleDay(f)
	}
	var manager *usageManager
	if w != nil {
		manager = w.usage
		if manager == nil {
			manager = existingUsageManager(w.dir)
		}
	}
	if manager != nil {
		if catalog := manager.catalog.Load(); catalog != nil {
			days := daysInRange(f.From, f.To)
			if catalog.Ready(context.Background(), w.dir, days) {
				if out, ok := rangeStatsFromCatalog(f, days, catalog); ok {
					return out, nil
				}
			}
			catalog.RequestReconcileDir(w.dir)
			catalog.NoteFallback()
		}
	}
	return w.queryJSONL(f)
}

// rangeStatsFromCatalog reads a ready projection, then the per-call facts the
// rollups keep only as sums: how many calls the money is over, whose money is
// unknown, and how each price was reached. Both come from the same stored
// rows, so this answers what the exact read answers.
func rangeStatsFromCatalog(f SourceFilter, days []string, catalog *usagecatalog.Catalog) (RangeStats, bool) {
	ctx := context.Background()
	from, to := f.From.Format(dayLayout), f.To.Format(dayLayout)
	rows, err := catalog.Query(ctx, from, to, f.Source, usagecatalog.CostView(f.Currency))
	if err != nil {
		return RangeStats{}, false
	}
	prov, err := catalog.ProvenanceInRange(ctx, from, to, f.Source)
	if err != nil {
		return RangeStats{}, false
	}
	out := rangeStatsFromRollups(f, days, rows)
	out.CountedCalls, out.UnpricedCalls, out.EstimatedCalls = prov.CountedCalls, prov.UnpricedCalls, prov.EstimatedCalls
	out.UnnamedCardCalls, out.UnclassifiedCardCalls = prov.UnnamedCardCalls, prov.UnclassifiedCardCalls
	out.IncompleteReasons = prov.Reasons
	out.CostIncomplete = prov.UnpricedCalls > 0
	return out, true
}

func emptyRange(f SourceFilter) RangeStats {
	return RangeStats{
		From:      f.From.Format(dayLayout),
		To:        f.To.Format(dayLayout),
		Daily:     []DailyTokens{},
		Models:    []ModelUsage{},
		Providers: []ProviderUsage{},
	}
}

func (w *Writer) queryJSONL(f SourceFilter) (RangeStats, error) {
	if w == nil || w.dir == "" {
		return emptyRange(f), nil
	}
	days := daysInRange(f.From, f.To)
	recordsByDay, err := readDailyRange(w.dir, days)
	if err != nil {
		return emptyRange(f), err
	}
	return aggregateDays(recordsByDay, days, f), nil
}

func aggregateDays(recordsByDay map[string][]record, days []string, f SourceFilter) RangeStats {
	out := emptyRange(f)
	view := usagecatalog.CostView(f.Currency)
	modelTotals := map[string]int64{}
	providerTotals := map[string]int64{}
	totalCost := newCostBooks()
	unpricedReasons := map[string]bool{}
	active := map[string]bool{} // day -> active
	for _, day := range days {
		recs := recordsByDay[day]
		dayCost := newCostBooks()
		dayTotals := map[string]int64{}
		dayTurns := 0
		dayRequests := 0
		dayCacheHit := int64(0)
		dayCacheMiss := int64(0)
		dayActive := false
		for _, rec := range recs {
			if !matchesSource(rec.Source, f.Source) {
				continue
			}
			if rec.Turn {
				dayTurns++
				continue
			}
			t := int64(rec.Total)
			// Tokens keeps the provider's TotalTokens value as-is (input +
			// output, provider-specific); the cache hit-rate is derived only
			// from the input side (CacheHit+CacheMiss), so the two denominators
			// never mix even when a provider reports totals that omit cache
			// tokens.
			out.Tokens += t
			out.CacheHit += int64(rec.CacheHit)
			out.CacheMiss += int64(rec.CacheMiss)
			dayCacheHit += int64(rec.CacheHit)
			dayCacheMiss += int64(rec.CacheMiss)
			requests := rec.Requests
			if rec.Total > 0 && requests <= 0 {
				// Rows written before request accounting existed represented one
				// successful provider call. Keep that legacy default while allowing
				// new request-only rows to carry tokens=0 and requests>0.
				requests = 1
			}
			if requests > 0 {
				out.Requests += requests
				dayRequests += requests
			}
			if rec.Total > 0 {
				model := rec.ModelRef
				if model == "" {
					model = "(unknown)"
				}
				modelTotals[model] += t
				providerTotals[providerOf(model)] += t
				dayTotals[model] += t
			}
			entry := usageEntry(day, rec)
			if currency, amount := entry.CostIn(view); currency != "" {
				mode := usagecatalog.CostMode(entry.BillingMode)
				dayCost.add(mode, currency, pricing.Amount(amount))
				totalCost.add(mode, currency, pricing.Amount(amount))
				out.CostEstimated = out.CostEstimated || (mode == "" && entry.CostEstimated)
			}
			countCall(&out, rec, entry, unpricedReasons)
			dayActive = dayActive || rec.Total > 0 || requests > 0
		}
		if dayActive {
			active[day] = true
		}
		// Turns are tallied here for every day of the range (turn markers are
		// matched before the token branch above), so turn-only days count too.
		out.Turns += dayTurns
		// Emit every day of the range so the trend chart shows the full
		// timeline; inactive days carry zero totals. The frontend trims the
		// left side of the chart on narrow containers instead of hiding days.
		byModel := make(map[string]int64, len(dayTotals))
		maps.Copy(byModel, dayTotals)
		byProvider := map[string]int64{}
		for m, v := range dayTotals {
			byProvider[providerOf(m)] += v
		}
		out.Daily = append(out.Daily, dailyRow(day, byModel, byProvider, dayCost,
			dayRequests, dayTurns, dayCacheHit, dayCacheMiss))
	}
	out.ActiveDays = len(active)
	out.IncompleteReasons = sortedReasons(unpricedReasons)
	out.Cost, out.EstimateCost = moneySorted(totalCost.billed), moneySorted(totalCost.estimate)
	out.Models = modelsSorted(modelTotals)
	out.Providers = providersSorted(providerTotals)
	if len(out.Models) > 0 {
		out.TopModel = out.Models[0].Model
	}
	if len(out.Providers) > 0 {
		out.TopProvider = out.Providers[0].Provider
	}
	if out.Tokens > 0 {
		for i := range out.Models {
			out.Models[i].Percent = float64(out.Models[i].Tokens) / float64(out.Tokens) * 100
		}
		for i := range out.Providers {
			out.Providers[i].Percent = float64(out.Providers[i].Tokens) / float64(out.Tokens) * 100
		}
	}
	sort.SliceStable(out.Daily, func(i, j int) bool { return out.Daily[i].Day < out.Daily[j].Day })
	return out
}

// countCall adds one row's provenance facts to the range: whether it is a
// counted call, and how its price was reached. Counted is the denominator the
// unpriced count is a subset of; a turn marker or a request-only failure (no
// tokens, no price, no failed quote) is not a call the money is over.
func countCall(out *RangeStats, rec record, entry usagecatalog.Entry, reasons map[string]bool) {
	counted := rec.Total > 0 || entry.CostCurrency != "" || entry.CostIncomplete
	if counted {
		out.CountedCalls++
	}
	if entry.CostIncomplete {
		out.CostIncomplete = true
		out.UnpricedCalls++
		if entry.IncompleteReason != "" {
			reasons[entry.IncompleteReason] = true
		}
	} else if entry.CostCurrency != "" && entry.CostEstimated {
		out.EstimatedCalls++
	}
	if counted {
		// No price fact at all: whether the card was named was never
		// recorded, which is unknown rather than zero.
		if entry.CostCurrency == "" {
			out.UnclassifiedCardCalls++
		} else if rec.PricingFingerprint == "" {
			out.UnnamedCardCalls++
		}
	}
}

// querySingleDay adds the hour buckets a day rollup cannot answer. One day's
// file is small enough to read whole rather than widen the catalog schema.
func (w *Writer) querySingleDay(f SourceFilter) (RangeStats, error) {
	if w == nil || w.dir == "" {
		return emptyRange(f), nil
	}
	day := f.From.Format(dayLayout)
	recs, err := readDaily(w.dir, day)
	if err != nil {
		return emptyRange(f), err
	}
	out := aggregateDays(map[string][]record{day: recs}, []string{day}, f)
	out.Hourly = hourlyBuckets(day, recs, f)
	return out, nil
}

func hourlyBuckets(day string, recs []record, f SourceFilter) []HourlyTokens {
	view := usagecatalog.CostView(f.Currency)
	buckets := make([]HourlyTokens, 24)
	costs := make([]costBooks, 24)
	for h := range buckets {
		buckets[h] = HourlyTokens{
			Hour:       fmt.Sprintf("%02d:00", h),
			ByModel:    map[string]int64{},
			ByProvider: map[string]int64{},
		}
		costs[h] = newCostBooks()
	}
	for _, rec := range recs {
		if !matchesSource(rec.Source, f.Source) {
			continue
		}
		hour := rec.Timestamp.Hour()
		bucket := &buckets[hour]
		if rec.Turn {
			bucket.Turns++
			continue
		}
		requests := rec.Requests
		if rec.Total > 0 && requests <= 0 {
			requests = 1
		}
		t := int64(rec.Total)
		bucket.Total += rec.Total
		bucket.Requests += requests
		bucket.CacheHit += int64(rec.CacheHit)
		bucket.CacheMiss += int64(rec.CacheMiss)
		if rec.Total > 0 {
			model := rec.ModelRef
			if model == "" {
				model = "(unknown)"
			}
			bucket.ByModel[model] += t
			bucket.ByProvider[providerOf(model)] += t
		}
		if currency, amount := usageEntry(day, rec).CostIn(view); currency != "" {
			costs[hour].add(usagecatalog.CostMode(rec.BillingMode), currency, pricing.Amount(amount))
		}
	}
	for h := range buckets {
		buckets[h].Cost, buckets[h].EstimateCost = moneySorted(costs[h].billed), moneySorted(costs[h].estimate)
	}
	return buckets
}

func rangeStatsFromRollups(f SourceFilter, days []string, rows []usagecatalog.Rollup) RangeStats {
	out := RangeStats{From: f.From.Format(dayLayout), To: f.To.Format(dayLayout), Daily: []DailyTokens{}, Models: []ModelUsage{}, Providers: []ProviderUsage{}}
	byDay := map[string][]usagecatalog.Rollup{}
	for _, row := range rows {
		byDay[row.Day] = append(byDay[row.Day], row)
	}
	modelTotals := map[string]int64{}
	providerTotals := map[string]int64{}
	totalCost := newCostBooks()
	active := map[string]bool{}
	for _, day := range days {
		dayCost := newCostBooks()
		dayModels := map[string]int64{}
		dayProviders := map[string]int64{}
		dayRequests, dayTurns := int64(0), int64(0)
		dayCacheHit, dayCacheMiss := int64(0), int64(0)
		for _, row := range byDay[day] {
			out.Tokens += row.Total
			out.Requests += int(row.Requests)
			out.Turns += int(row.Turns)
			out.CacheHit += row.CacheHit
			out.CacheMiss += row.CacheMiss
			dayRequests += row.Requests
			dayTurns += row.Turns
			dayCacheHit += row.CacheHit
			dayCacheMiss += row.CacheMiss
			if row.Total > 0 {
				model := row.ModelRef
				if model == "" {
					model = "(unknown)"
				}
				provider := row.Provider
				if provider == "" {
					provider = providerOf(model)
				}
				modelTotals[model] += row.Total
				providerTotals[provider] += row.Total
				dayModels[model] += row.Total
				dayProviders[provider] += row.Total
			}
			for _, entry := range row.Costs {
				currency := pricing.NormalizeCurrency(entry.Currency)
				if currency == "" {
					continue
				}
				// A currency with a zero amount is a priced free quote: the
				// exact JSONL read reports {currency 0}, and so does this one,
				// including its estimate flag.
				dayCost.add(entry.Mode, currency, pricing.Amount(entry.Amount))
				totalCost.add(entry.Mode, currency, pricing.Amount(entry.Amount))
				out.CostEstimated = out.CostEstimated || (entry.Mode == "" && entry.Estimated)
			}
			if row.CostIncomplete > 0 {
				out.CostIncomplete = true
			}
			if row.Total > 0 || row.Requests > 0 {
				active[day] = true
			}
		}
		out.Daily = append(out.Daily, DailyTokens{Day: day, Total: int(sum(dayModels)), ByModel: dayModels, ByProvider: dayProviders,
			Requests: int(dayRequests), Turns: int(dayTurns), CacheHit: dayCacheHit, CacheMiss: dayCacheMiss,
			Cost: moneySorted(dayCost.billed), EstimateCost: moneySorted(dayCost.estimate)})
	}
	out.ActiveDays = len(active)
	out.Cost, out.EstimateCost = moneySorted(totalCost.billed), moneySorted(totalCost.estimate)
	out.Models = modelsSorted(modelTotals)
	out.Providers = providersSorted(providerTotals)
	if len(out.Models) > 0 {
		out.TopModel = out.Models[0].Model
	}
	if len(out.Providers) > 0 {
		out.TopProvider = out.Providers[0].Provider
	}
	if out.Tokens > 0 {
		for i := range out.Models {
			out.Models[i].Percent = float64(out.Models[i].Tokens) / float64(out.Tokens) * 100
		}
		for i := range out.Providers {
			out.Providers[i].Percent = float64(out.Providers[i].Tokens) / float64(out.Tokens) * 100
		}
	}
	return out
}

// dailyRow assembles one day of the trend series.
func dailyRow(day string, byModel, byProvider map[string]int64, cost costBooks,
	requests, turns int, cacheHit, cacheMiss int64,
) DailyTokens {
	return DailyTokens{
		Day: day, Total: int(sum(byModel)), ByModel: byModel, ByProvider: byProvider,
		Requests: requests, Turns: turns, CacheHit: cacheHit, CacheMiss: cacheMiss,
		Cost: moneySorted(cost.billed), EstimateCost: moneySorted(cost.estimate),
	}
}

// costBooks tallies cost per currency, billed and estimated apart.
type costBooks struct{ billed, estimate map[string]pricing.Amount }

func newCostBooks() costBooks {
	return costBooks{billed: map[string]pricing.Amount{}, estimate: map[string]pricing.Amount{}}
}

func (b costBooks) add(mode, currency string, amount pricing.Amount) {
	if mode == usagecatalog.ModeEstimate {
		b.estimate[currency] += amount
		return
	}
	b.billed[currency] += amount
}

// moneySorted renders the tally in a stable currency order so a repeated query
// returns the same JSON.
func moneySorted(tally map[string]pricing.Amount) []pricing.Money {
	if len(tally) == 0 {
		return nil
	}
	out := make([]pricing.Money, 0, len(tally))
	for currency, amount := range tally {
		out = append(out, pricing.MoneyOf(amount, currency))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

// sortedReasons renders the distinct reasons in a stable order so a repeated
// query returns the same JSON. No unnamed reason enters the list.
func sortedReasons(seen map[string]bool) []string {
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for reason := range seen {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

// matchesSource reports whether a record's source label passes the filter.
// An empty filter or "all" matches every source.
func matchesSource(recSource, filter string) bool {
	if filter == "" || filter == "all" {
		return true
	}
	return recSource == filter
}

func providerOf(modelRef string) string {
	// model refs are "provider/model"; a bare model name (legacy configs) has
	// no slash and is attributed to provider "default".
	if i := strings.IndexByte(modelRef, '/'); i > 0 {
		return modelRef[:i]
	}
	return "default"
}

func sum(m map[string]int64) int64 {
	var s int64
	for _, v := range m {
		s += v
	}
	return s
}

func modelsSorted(totals map[string]int64) []ModelUsage {
	out := make([]ModelUsage, 0, len(totals))
	for model, t := range totals {
		out = append(out, ModelUsage{Model: model, Provider: providerOf(model), Tokens: t})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Tokens == out[j].Tokens {
			return out[i].Model < out[j].Model
		}
		return out[i].Tokens > out[j].Tokens
	})
	return out
}

func providersSorted(totals map[string]int64) []ProviderUsage {
	out := make([]ProviderUsage, 0, len(totals))
	for prov, t := range totals {
		out = append(out, ProviderUsage{Provider: prov, Tokens: t})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Tokens == out[j].Tokens {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Tokens > out[j].Tokens
	})
	return out
}
