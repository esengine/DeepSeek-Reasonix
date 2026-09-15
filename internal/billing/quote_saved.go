package billing

import (
	"fmt"
	"strings"
)

// CacheSavedAmount computes the fixed-point savings from prefix cache hits.
func CacheSavedAmount(rates RateCard, u UsageTokens) Amount {
	if u.CacheHitTokens <= 0 || rates.Input <= rates.CacheHit {
		return Zero
	}
	saved := float64(u.CacheHitTokens) * (rates.Input - rates.CacheHit) / 1e6
	return NewAmountFromFloat(saved)
}

// FormatSavedFeedback formats a savings feedback message like "saved ¥0.12 via prefix cache".
func FormatSavedFeedback(m Money) string {
	val := m.Float64()
	if val <= 0 {
		return ""
	}
	sym := CurrencySymbol(m.Currency)
	if val >= 0.01 {
		return fmt.Sprintf("saved %s%.2f via prefix cache", sym, val)
	}
	return fmt.Sprintf("saved %s%.4f via prefix cache", sym, val)
}

// SavedFeedback returns the formatted savings feedback for the quote's Saved amount,
// or "" if no savings were recorded.
func (q CostQuote) SavedFeedback() string {
	if q.Saved == nil {
		return ""
	}
	return FormatSavedFeedback(*q.Saved)
}

// NormalizeQuote fills fields introduced after the first CostQuote wire shape.
// It is used at every persistence and transport boundary so old telemetry and
// old eventwire payloads remain safe without inventing a new amount.
func NormalizeQuote(q CostQuote) CostQuote {
	if !q.CostComplete && !q.DisplayComplete && q.Complete {
		q.CostComplete = true
		q.DisplayComplete = true
	}
	if q.DisplayStatus != "" && q.DisplayStatus != DisplayStatusMatched && q.DisplayStatus != DisplayStatusFallbackOriginal && q.DisplayStatus != DisplayStatusBucketed && q.DisplayStatus != DisplayStatusUnavailable {
		q.DisplayStatus = ""
	}
	if q.AggregateMode != "" && q.AggregateMode != AggregateModeSingleCurrency && q.AggregateMode != AggregateModeCommonValuation && q.AggregateMode != AggregateModeCurrencyBuckets {
		q.AggregateMode = ""
	}
	if q.RateBand != "" && q.RateBand != RateBandPeak && q.RateBand != RateBandOffPeak && q.RateBand != RateBandMixed {
		q.RateBand = ""
	}
	if q.DisplayStatus == "" {
		switch {
		case q.Complete:
			q.DisplayStatus = DisplayStatusMatched
		case q.Original.Currency != "" && q.Original.Amount != "" && q.IncompleteReason != "no_price":
			q.DisplayStatus = DisplayStatusFallbackOriginal
		default:
			q.DisplayStatus = DisplayStatusUnavailable
		}
	}
	if q.AggregateMode == "" {
		if len(q.OriginalTotals) > 1 {
			q.AggregateMode = AggregateModeCurrencyBuckets
		} else if q.Selected != nil {
			q.AggregateMode = AggregateModeSingleCurrency
		}
	}
	q.Complete = q.DisplayComplete
	return q
}

// quoteHasCompleteCostFact separates a known original-currency charge from a
// display-only valuation failure must not poison an exact original total,
// while unpriced and unrecoverable legacy records stay incomplete in every
// display currency.
func quoteHasCompleteCostFact(q CostQuote) bool {
	if q.CostComplete {
		return true
	}
	switch q.IncompleteReason {
	case "no_price", "missing_price_or_usage", "legacy_unrecoverable",
		"legacy_wiped_or_zero", "legacy_invalid_amount", "mixed_original_currencies",
		"incomplete_cost_fact":
		return false
	}
	if q.Complete {
		return true
	}
	currency := NormalizeCurrency(q.Original.Currency)
	if currency == "" {
		return false
	}
	valuation, ok := q.Valuations[currency]
	return ok && SameCurrency(valuation.Money.Currency, currency)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func resolveCatalogIdentity(in QuoteInput) (provider, model string) {
	provider = strings.ToLower(strings.TrimSpace(in.ProviderKind))
	model = strings.TrimSpace(in.ModelID)
	if model == "" {
		ref := strings.TrimSpace(in.ModelRef)
		if i := strings.LastIndex(ref, "/"); i >= 0 && i+1 < len(ref) {
			model = ref[i+1:]
			if provider == "" {
				provider = strings.ToLower(ref[:i])
			}
		} else {
			model = ref
		}
	}
	// Normalize common provider name prefixes.
	switch {
	case strings.Contains(provider, "deepseek"):
		provider = "deepseek"
	case strings.Contains(provider, "longcat"):
		provider = "longcat"
	case strings.Contains(provider, "mimo"):
		provider = "mimo"
	}
	if provider == "" {
		// Infer from model id family.
		switch {
		case strings.HasPrefix(model, "deepseek"):
			provider = "deepseek"
		case strings.HasPrefix(model, "LongCat") || strings.HasPrefix(model, "longcat"):
			provider = "longcat"
		case strings.HasPrefix(model, "mimo"):
			provider = "mimo"
		}
	}
	if provider == "deepseek" {
		switch model {
		case "deepseek", "deepseek-chat", "":
			model = "deepseek-v4-flash"
		case "deepseek-reasoner":
			model = "deepseek-v4-pro"
		}
	}
	return provider, model
}
