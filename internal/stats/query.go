package stats

import (
	"maps"
	"sort"
	"strings"
	"time"
)

// DailyTokens is one day's token usage and turn count in a trend series.
type DailyTokens struct {
	Day        string           `json:"day"` // "2026-08-02"
	Total      int              `json:"total"`
	ByModel    map[string]int64 `json:"byModel"`    // model ref -> tokens
	ByProvider map[string]int64 `json:"byProvider"` // provider name -> tokens
	Requests   int              `json:"requests"`   // provider API requests
	Turns      int              `json:"turns"`      // completed turns
	CacheHit   int64            `json:"cacheHit"`   // cached input tokens that day
	CacheMiss  int64            `json:"cacheMiss"`  // uncached input tokens that day
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
	ActiveDays  int    `json:"active_days"`
	TopModel    string `json:"top_model"`
	TopProvider string `json:"top_provider"`
	// Series
	Daily     []DailyTokens   `json:"daily"`
	Models    []ModelUsage    `json:"models"`
	Providers []ProviderUsage `json:"providers"`
}

// SourceFilter selects which source labels to aggregate; "" or "all" includes
// every source.
type SourceFilter struct {
	Source string
	From   time.Time
	To     time.Time
}

// Query aggregates the daily stats files intersecting [from, to]. Missing days
// yield zero entries. When SourceFilter.Source is set, only records whose
// Source matches are counted.
func (w *Writer) Query(f SourceFilter) (RangeStats, error) {
	out := RangeStats{
		From:      f.From.Format(dayLayout),
		To:        f.To.Format(dayLayout),
		Daily:     []DailyTokens{},
		Models:    []ModelUsage{},
		Providers: []ProviderUsage{},
	}
	if w == nil || w.dir == "" {
		return out, nil
	}
	days := daysInRange(f.From, f.To)
	recordsByDay, err := readDailyRange(w.dir, days)
	if err != nil {
		return out, err
	}
	modelTotals := map[string]int64{}
	providerTotals := map[string]int64{}
	active := map[string]bool{} // day -> active
	for _, day := range days {
		recs := recordsByDay[day]
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
		out.Daily = append(out.Daily, DailyTokens{
			Day:        day,
			Total:      int(sum(dayTotals)),
			ByModel:    byModel,
			ByProvider: byProvider,
			Requests:   dayRequests,
			Turns:      dayTurns,
			CacheHit:   dayCacheHit,
			CacheMiss:  dayCacheMiss,
		})
	}
	out.ActiveDays = len(active)
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
	return out, nil
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
