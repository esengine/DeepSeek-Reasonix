package billing

import (
	"maps"
	"sort"
	"strings"
	"time"
)

// LedgerEntry is one occurrence-time cost fact. Ledger keys include model,
// usage source, pricing fingerprint, and legacy rate date so model switches and
// sub-agents never collapse into a single scalar.
type LedgerEntry struct {
	Key                string    `json:"key"`
	ModelRef           string    `json:"modelRef"`
	UsageSource        string    `json:"usageSource"`
	PricingFingerprint string    `json:"pricingFingerprint"`
	RateDate           string    `json:"rateDate,omitempty"`
	OccurredAt         time.Time `json:"occurredAt"`
	Quote              CostQuote `json:"quote"`
	// Token totals for this bucket (summed).
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
	CacheHitTokens   int `json:"cacheHitTokens"`
	CacheMissTokens  int `json:"cacheMissTokens"`
	RequestCount     int `json:"requestCount"`
}

// LedgerKey builds the stable aggregation key.
func LedgerKey(modelRef, usageSource, pricingFingerprint, rateDate string) string {
	return strings.Join([]string{
		strings.TrimSpace(modelRef),
		strings.TrimSpace(usageSource),
		strings.TrimSpace(pricingFingerprint),
		strings.TrimSpace(rateDate),
	}, "|")
}

// Ledger accumulates occurrence-time quotes. Switching display currency only
// re-selects valuations already stored; it does not reprice or clear history.
type Ledger struct {
	Version int                    `json:"version"`
	Entries map[string]LedgerEntry `json:"entries"`
}

// LedgerVersion is the current persisted ledger schema.
const LedgerVersion = 1

// NewLedger returns an empty ledger.
func NewLedger() *Ledger {
	return &Ledger{Version: LedgerVersion, Entries: map[string]LedgerEntry{}}
}

// Add merges a quote into the ledger under its natural key.
func (l *Ledger) Add(q CostQuote, tokens UsageTokens, occurred time.Time) {
	if l == nil {
		return
	}
	if l.Entries == nil {
		l.Entries = map[string]LedgerEntry{}
	}
	if l.Version == 0 {
		l.Version = LedgerVersion
	}
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	q = NormalizeQuote(q)
	source := q.UsageSource
	if source == "" {
		source = "executor"
	}
	key := LedgerKey(q.ModelRef, source, q.PricingFingerprint, q.RateDate)
	ent, ok := l.Entries[key]
	if !ok {
		ent = LedgerEntry{
			Key:                key,
			ModelRef:           q.ModelRef,
			UsageSource:        source,
			PricingFingerprint: q.PricingFingerprint,
			RateDate:           q.RateDate,
			OccurredAt:         occurred,
			Quote:              q,
		}
		// Fresh quote valuations are kept; we re-aggregate Original via sums.
		ent.Quote.Valuations = cloneValuations(q.Valuations)
	} else {
		mergeLedgerEntryQuote(&ent.Quote, q)
	}
	ent.PromptTokens += tokens.PromptTokens
	ent.CompletionTokens += tokens.CompletionTokens
	ent.TotalTokens += tokens.PromptTokens + tokens.CompletionTokens
	if tokens.CacheHitTokens+tokens.CacheMissTokens > 0 {
		ent.CacheHitTokens += tokens.CacheHitTokens
		ent.CacheMissTokens += tokens.CacheMissTokens
	} else {
		ent.CacheMissTokens += tokens.PromptTokens
	}
	ent.RequestCount++
	if occurred.After(ent.OccurredAt) {
		ent.OccurredAt = occurred
	}
	l.Entries[key] = ent
}

func mergeLedgerEntryQuote(target *CostQuote, q CostQuote) {
	// A bucket with more than one occurrence no longer has a single rating
	// instant even though its fingerprint keeps the rate band homogeneous.
	target.RatedAt = ""
	// Sum original when same currency; otherwise retain deterministic
	// per-currency buckets. Once a bucketed entry exists, keep adding into
	// those buckets so later same-currency calls are not lost.
	if len(target.OriginalTotals) > 0 {
		target.OriginalTotals = mergeOriginalTotals(target.OriginalTotals, q)
		target.CostComplete = true
		target.DisplayComplete = false
		target.Complete = false
		target.DisplayStatus = DisplayStatusBucketed
		target.AggregateMode = AggregateModeCurrencyBuckets
		target.IncompleteReason = "mixed_original_currencies"
	} else {
		sum, err := AddMoney(target.Original, q.Original)
		if err != nil {
			target.OriginalTotals = mergeOriginalTotals([]Money{target.Original}, q)
			target.CostComplete = true
			target.DisplayComplete = false
			target.Complete = false
			target.DisplayStatus = DisplayStatusBucketed
			target.AggregateMode = AggregateModeCurrencyBuckets
			target.IncompleteReason = "mixed_original_currencies"
		} else {
			target.Original = sum
			if target.Saved != nil && q.Saved != nil {
				if savedSum, err := AddMoney(*target.Saved, *q.Saved); err == nil {
					target.Saved = &savedSum
				}
			} else if q.Saved != nil {
				target.Saved = q.Saved
			}
		}
	}
	for code, v := range q.Valuations {
		code = NormalizeCurrency(code)
		if prev, ok := target.Valuations[code]; ok {
			added, err := AddMoney(prev.Money, v.Money)
			if err == nil {
				prev.Money = added
				if prev.Saved != nil && v.Saved != nil {
					if savedSum, err := AddMoney(*prev.Saved, *v.Saved); err == nil {
						prev.Saved = &savedSum
					}
				} else if v.Saved != nil {
					prev.Saved = v.Saved
				}
				if v.Stale {
					prev.Stale = true
				}
				target.Valuations[code] = prev
			}
		} else {
			if target.Valuations == nil {
				target.Valuations = map[string]Valuation{}
			}
			target.Valuations[code] = v
		}
	}
	if q.Estimated {
		target.Estimated = true
	}
	if !q.Complete {
		target.DisplayComplete = false
		target.Complete = false
		if target.IncompleteReason == "" {
			target.IncompleteReason = q.IncompleteReason
		}
	}
	target.CostComplete = target.CostComplete && q.CostComplete
	if target.DisplayStatus != DisplayStatusBucketed {
		target.DisplayStatus = q.DisplayStatus
	}
}

func mergeOriginalTotals(existing []Money, q CostQuote) []Money {
	amounts := map[string]Amount{}
	add := func(m Money) {
		currency := NormalizeCurrency(m.Currency)
		if currency == "" {
			return
		}
		amounts[currency] = amounts[currency].Add(m.AmountValue())
	}
	for _, m := range existing {
		add(m)
	}
	if len(q.OriginalTotals) > 0 {
		for _, m := range q.OriginalTotals {
			add(m)
		}
	} else {
		add(q.Original)
	}
	codes := make([]string, 0, len(amounts))
	for code := range amounts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	out := make([]Money, 0, len(codes))
	for _, code := range codes {
		out = append(out, MoneyOf(amounts[code], code))
	}
	return out
}

func cloneValuations(in map[string]Valuation) map[string]Valuation {
	if len(in) == 0 {
		return map[string]Valuation{}
	}
	out := make(map[string]Valuation, len(in))
	maps.Copy(out, in)
	return out
}

// Total returns the aggregate CostQuote for a display currency.
func (l *Ledger) Total(display string) CostQuote {
	if l == nil || len(l.Entries) == 0 {
		return AggregateQuotes(nil, display)
	}
	quotes := make([]CostQuote, 0, len(l.Entries))
	for _, ent := range l.Entries {
		q := ent.Quote
		// Refresh valuation moneys already summed in entries.
		quotes = append(quotes, q)
	}
	// Stable order for determinism.
	sort.Slice(quotes, func(i, j int) bool {
		return quotes[i].ModelRef+quotes[i].UsageSource < quotes[j].ModelRef+quotes[j].UsageSource
	})
	return AggregateQuotes(quotes, display)
}

// SelectDisplay rebinds Selected on every entry and the total without
// recomputing occurrence-time pricing facts.
func (l *Ledger) SelectDisplay(display string) CostQuote {
	return l.Total(display)
}

// EntriesBySource groups ledger entries by usage source for status panels.
func (l *Ledger) EntriesBySource() map[string][]LedgerEntry {
	out := map[string][]LedgerEntry{}
	if l == nil {
		return out
	}
	for _, ent := range l.Entries {
		out[ent.UsageSource] = append(out[ent.UsageSource], ent)
	}
	return out
}
