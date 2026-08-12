package billing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Billing modes.
const (
	BillingModePAYG                   = "payg"
	BillingModeSubscriptionEquivalent = "subscription_equivalent"
)

// Valuation basis values.
const (
	BasisIdentity      = "identity"
	BasisOfficialTable = "official_table"
	// BasisFX is retained for decoding pre-v6 persisted quotes only. New
	// quotes never create FX valuations.
	BasisFX = "fx"
)

// DisplayStatus describes whether a quote can satisfy its requested display.
// It is deliberately separate from CostComplete: a known price-book amount
// can exist even when no requested regional valuation is available.
const (
	DisplayStatusMatched          = "matched"
	DisplayStatusFallbackOriginal = "fallback_original"
	DisplayStatusBucketed         = "bucketed"
	DisplayStatusUnavailable      = "unavailable"
)

// Aggregate modes describe how a session/run total was formed.
const (
	AggregateModeSingleCurrency  = "single_currency"
	AggregateModeCommonValuation = "common_valuation"
	AggregateModeCurrencyBuckets = "currency_buckets"
)

// DisplayRequest identifies how a caller selected the requested presentation
// currency. The source is runtime context only; it is never persisted as a
// provider price or wallet fact.
type DisplayRequest struct {
	Currency string `json:"currency,omitempty"`
	Source   string `json:"source,omitempty"`
}

const (
	DisplaySourceExplicit = "explicit"
	DisplaySourceWallet   = "wallet"
	DisplaySourceAuto     = "auto"
)

func (r DisplayRequest) NormalizedCurrency() string {
	return NormalizeCurrency(r.Currency)
}

// CostQuote is the host-side quoted cost for one model call (or an aggregate).
// Original is the pricing-table currency fact; valuations hold identity or
// official regional rate-card estimates. Selected is the display pick for the
// current preference.
type CostQuote struct {
	Original Money `json:"original"`
	// OriginalTotals is populated for mixed-currency aggregates. Individual
	// quotes and single-currency totals may omit it.
	OriginalTotals []Money              `json:"originalTotals,omitempty"`
	Valuations     map[string]Valuation `json:"valuations,omitempty"`
	Selected       *Money               `json:"selected,omitempty"`
	BillingMode    string               `json:"billingMode,omitempty"`
	Estimated      bool                 `json:"estimated"`
	// CostComplete means usage and the price-book amount are known. It does not
	// imply that a requested display currency is available.
	CostComplete bool `json:"costComplete"`
	// DisplayComplete means a single selected amount satisfies the display
	// request. Complete is retained as the old wire alias.
	DisplayComplete bool   `json:"displayComplete"`
	Complete        bool   `json:"complete"`
	DisplayStatus   string `json:"displayStatus,omitempty"`
	AggregateMode   string `json:"aggregateMode,omitempty"`
	// ModelRef / UsageSource / PricingFingerprint identify the ledger key.
	ModelRef           string `json:"modelRef,omitempty"`
	UsageSource        string `json:"usageSource,omitempty"`
	PricingFingerprint string `json:"pricingFingerprint,omitempty"`
	RateDate           string `json:"rateDate,omitempty"` // YYYY-MM-DD of FX used, if any
	IncompleteReason   string `json:"incompleteReason,omitempty"`
	LegacyEstimate     bool   `json:"legacyEstimate,omitempty"`
	CatalogSource      string `json:"catalogSource,omitempty"`
}

// Valuation is one currency view of a cost fact.
type Valuation struct {
	Money  Money         `json:"money"`
	Basis  string        `json:"basis"`
	Source string        `json:"source"`
	AsOf   string        `json:"asOf"` // YYYY-MM-DD
	Rate   *RateSnapshot `json:"rateSnapshot,omitempty"`
	Stale  bool          `json:"stale,omitempty"`
}

// RateSnapshot records the FX observation used for a valuation.
type RateSnapshot struct {
	Base   string  `json:"base"`
	Quote  string  `json:"quote"`
	Rate   float64 `json:"rate"`
	Source string  `json:"source"`
	AsOf   string  `json:"asOf"`
	Stale  bool    `json:"stale,omitempty"`
}

// RateCard is the per-1M-token price used to compute original cost. It mirrors
// provider.Pricing without importing that package (billing is a leaf).
type RateCard struct {
	CacheHit float64 // per 1M cached prompt tokens
	Input    float64 // per 1M uncached prompt tokens
	Output   float64 // per 1M completion tokens
	Currency string  // ISO or symbol; normalized on quote
}

// UsageTokens is the token breakdown needed for cost. Mirrors provider.Usage
// fields without importing provider.
type UsageTokens struct {
	PromptTokens           int
	CompletionTokens       int
	CacheHitTokens         int
	CacheMissTokens        int
	CacheWriteTokens       int
	CacheWriteBilledTokens float64
	Estimated              bool
}

// QuoteInput drives a single CostQuote computation.
type QuoteInput struct {
	Usage              UsageTokens
	Rates              RateCard
	OccurredAt         time.Time
	DisplayCurrency    string         // compatibility alias for Display.Currency
	Display            DisplayRequest // runtime display request; empty means auto
	BillingMode        string
	ModelRef           string
	UsageSource        string
	CatalogSource      string
	PricingFingerprint string
	// ProviderKind is deepseek|longcat|mimo|… for official dual-table lookup.
	// When empty, ModelRef and Rates are used to infer a catalog match.
	ProviderKind string
	// ModelID is the bare model id (e.g. deepseek-v4-flash). Empty → parse ModelRef.
	ModelID string
}

// OriginalCostAmount computes the fixed-point original cost from rates + tokens.
// Mirrors the historic provider.Pricing.Cost semantics.
func OriginalCostAmount(rates RateCard, u UsageTokens) Amount {
	hit := u.CacheHitTokens
	miss := u.CacheMissTokens
	if hit+miss == 0 && u.PromptTokens > 0 {
		miss = u.PromptTokens
	} else if miss == 0 && hit > 0 && u.PromptTokens > hit {
		miss = u.PromptTokens - hit
	}
	write := u.CacheWriteTokens
	write = max(write, 0)
	write = min(write, miss)
	billedWrite := 0.0
	if write > 0 {
		billedWrite = u.CacheWriteBilledTokens
		if billedWrite <= 0 {
			billedWrite = float64(write)
		}
	}
	inputTokenUnits := float64(miss-write) + billedWrite
	// Combine cached input, uncached input, and output charges per million tokens.
	total := (float64(hit)*rates.CacheHit +
		inputTokenUnits*rates.Input +
		float64(u.CompletionTokens)*rates.Output) / 1e6
	return NewAmountFromFloat(total)
}

// PricingFingerprint hashes a rate card for ledger keys.
func PricingFingerprint(rates RateCard) string {
	cur := NormalizeCurrency(rates.Currency)
	raw := fmt.Sprintf("%s|%.12g|%.12g|%.12g", cur, rates.CacheHit, rates.Input, rates.Output)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

// BuildQuote computes original cost and CNY/USD valuations.
func BuildQuote(in QuoteInput) CostQuote {
	state := newQuoteBuildState(in)
	for _, target := range []string{"CNY", "USD"} {
		state.addTargetValuation(target)
	}
	state.selectDisplay()
	return state.quote
}

type quoteBuildState struct {
	input             QuoteInput
	currency          string
	amount            Amount
	occurred          time.Time
	official          CatalogEntry
	isOfficial        bool
	valuationFailures map[string]string
	quote             CostQuote
}

func newQuoteBuildState(in QuoteInput) *quoteBuildState {
	currency := NormalizeCurrency(in.Rates.Currency)
	if currency == "" {
		currency = "CNY"
	}
	mode := strings.TrimSpace(in.BillingMode)
	if mode == "" {
		mode = BillingModePAYG
	}
	occurred := in.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	fingerprint := strings.TrimSpace(in.PricingFingerprint)
	if fingerprint == "" {
		fingerprint = PricingFingerprint(in.Rates)
	}
	amount := OriginalCostAmount(in.Rates, in.Usage)
	q := CostQuote{
		Original:           MoneyOf(amount, currency),
		Valuations:         map[string]Valuation{},
		BillingMode:        mode,
		Estimated:          true,
		CostComplete:       true,
		DisplayComplete:    true,
		Complete:           true,
		DisplayStatus:      DisplayStatusMatched,
		AggregateMode:      AggregateModeSingleCurrency,
		ModelRef:           strings.TrimSpace(in.ModelRef),
		UsageSource:        strings.TrimSpace(in.UsageSource),
		PricingFingerprint: fingerprint,
		CatalogSource:      strings.TrimSpace(in.CatalogSource),
	}
	q.Valuations[currency] = Valuation{
		Money: q.Original, Basis: BasisIdentity,
		Source: firstNonEmpty(in.CatalogSource, "rate_card"),
		AsOf:   occurred.UTC().Format("2006-01-02"),
	}
	state := &quoteBuildState{
		input: in, currency: currency, amount: amount, occurred: occurred,
		valuationFailures: map[string]string{}, quote: q,
	}
	if !usageHasFacts(in.Usage) {
		state.markIncomplete("missing_price_or_usage")
	}
	state.matchOfficialCatalog()
	return state
}

func quoteDisplayRequest(in QuoteInput) DisplayRequest {
	request := in.Display
	if request.Currency == "" && in.DisplayCurrency != "" {
		request.Currency = in.DisplayCurrency
		if request.Source == "" {
			request.Source = DisplaySourceExplicit
		}
	}
	if request.Source == "" {
		request.Source = DisplaySourceAuto
	}
	return request
}

func usageHasFacts(u UsageTokens) bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.CacheHitTokens > 0 ||
		u.CacheMissTokens > 0 || u.CacheWriteTokens > 0 || u.CacheWriteBilledTokens > 0
}

func (s *quoteBuildState) matchOfficialCatalog() {
	providerKind, modelID := resolveCatalogIdentity(s.input)
	s.official, s.isOfficial = MatchesCatalog(providerKind, modelID, s.input.Rates)
	if !s.isOfficial && providerKind != "" && modelID != "" {
		rates := s.input.Rates
		rates.Currency = s.currency
		s.official, s.isOfficial = MatchesCatalog(providerKind, modelID, rates)
	}
	if s.isOfficial && s.quote.CatalogSource == "" {
		s.quote.CatalogSource = s.official.DocURL
	}
}

func (s *quoteBuildState) addTargetValuation(target string) {
	if target == s.currency {
		return
	}
	if s.addOfficialValuation(target) {
		return
	}
	// Runtime FX has intentionally been removed. A custom price that does not
	// match the official catalog remains in its original currency.
	s.valuationFailures[target] = "display_unavailable"
}

func (s *quoteBuildState) addOfficialValuation(target string) bool {
	if !s.isOfficial {
		return false
	}
	peer, ok := LookupCatalog(
		s.official.Provider, s.official.Model, target, s.official.BillingMode,
	)
	if !ok {
		return false
	}
	s.quote.Valuations[target] = Valuation{
		Money: MoneyOf(OriginalCostAmount(RateCardFromCatalog(peer), s.input.Usage), target),
		Basis: BasisOfficialTable, Source: peer.DocURL,
		AsOf: s.occurred.UTC().Format("2006-01-02"),
	}
	return true
}

func (s *quoteBuildState) selectDisplay() {
	if !s.quote.CostComplete {
		s.quote.Selected = nil
		s.quote.DisplayComplete = false
		s.quote.Complete = false
		s.quote.DisplayStatus = DisplayStatusUnavailable
		return
	}
	display := quoteDisplayRequest(s.input).NormalizedCurrency()
	if display == "" {
		selected := s.quote.Original
		s.quote.Selected = &selected
		return
	}
	if valuation, ok := s.quote.Valuations[display]; ok {
		selected := valuation.Money
		s.quote.Selected = &selected
		return
	}
	// The original price-book amount is still a valid cost fact. Keep it as a
	// visible fallback and distinguish the display mismatch from no pricing.
	selected := s.quote.Original
	s.quote.Selected = &selected
	s.quote.DisplayComplete = false
	s.quote.Complete = false
	s.quote.DisplayStatus = DisplayStatusFallbackOriginal
	s.quote.IncompleteReason = firstNonEmpty(s.valuationFailures[display], "display_unavailable")
}

func (s *quoteBuildState) markIncomplete(reason string) {
	s.quote.CostComplete = false
	s.quote.DisplayComplete = false
	s.quote.Complete = false
	s.quote.DisplayStatus = DisplayStatusUnavailable
	if s.quote.IncompleteReason == "" {
		s.quote.IncompleteReason = reason
	}
}

// SelectForDisplay returns the money for a display currency preference without
// recomputing rates. Empty display returns original.
func (q CostQuote) SelectForDisplay(display string) (Money, bool) {
	display = NormalizeCurrency(display)
	if display == "" {
		return q.Original, true
	}
	if v, ok := q.Valuations[display]; ok {
		return v.Money, true
	}
	if NormalizeCurrency(q.Original.Currency) == display {
		return q.Original, true
	}
	return Money{}, false
}

// WithSelected returns a copy with Selected set for display.
func (q CostQuote) WithSelected(display string) CostQuote {
	out := q
	if m, ok := q.SelectForDisplay(display); ok {
		out.Selected = &m
		if quoteHasCompleteCostFact(q) {
			out.Complete = true
			out.IncompleteReason = ""
		}
	} else {
		out.Selected = nil
		out.Complete = false
		if out.IncompleteReason == "" {
			out.IncompleteReason = "display_unavailable"
		}
	}
	return out
}

// LegacyCostFloat returns the selected (or original) amount as float64 for
// compatibility aliases cost / costUsd / total_cost.
func (q CostQuote) LegacyCostFloat() float64 {
	if q.Selected != nil {
		return q.Selected.Float64()
	}
	return q.Original.Float64()
}

// LegacyCurrencySymbol returns a display symbol for the selected/original currency.
func (q CostQuote) LegacyCurrencySymbol() string {
	if q.Selected != nil && q.Selected.Currency != "" {
		return CurrencySymbol(q.Selected.Currency)
	}
	return CurrencySymbol(q.Original.Currency)
}

// LegacyCurrencyCode returns the ISO code for selected/original.
func (q CostQuote) LegacyCurrencyCode() string {
	if q.Selected != nil && q.Selected.Currency != "" {
		return NormalizeCurrency(q.Selected.Currency)
	}
	return NormalizeCurrency(q.Original.Currency)
}

// AggregateQuotes combines occurrence-time quotes into a session/run total.
// Every entry must have the requested valuation; partial totals stay unavailable.
func AggregateQuotes(quotes []CostQuote, display string) CostQuote {
	if len(quotes) == 0 {
		return emptyAggregate(NormalizeCurrency(display))
	}
	accumulator := newQuoteAccumulator(display)
	for _, quote := range quotes {
		accumulator.add(quote)
	}
	return accumulator.finish()
}

type quoteAccumulator struct {
	out               CostQuote
	display           string
	totals            map[string]Amount
	originalCurrency  string
	originalTotal     Amount
	originalTotals    map[string]Amount
	originalComplete  bool
	costFactsComplete bool
	displayComplete   bool
	modes             map[string]struct{}
}

func emptyAggregate(display string) CostQuote {
	out := CostQuote{
		Original: MoneyOf(Zero, display), Valuations: map[string]Valuation{}, Estimated: true,
		CostComplete: false, DisplayComplete: false, Complete: false,
		DisplayStatus: DisplayStatusUnavailable, AggregateMode: AggregateModeSingleCurrency,
		IncompleteReason: "no_usage",
	}
	return out
}

func newQuoteAccumulator(display string) *quoteAccumulator {
	return &quoteAccumulator{
		out:     CostQuote{Valuations: map[string]Valuation{}, Estimated: true},
		display: NormalizeCurrency(display), totals: map[string]Amount{}, originalTotals: map[string]Amount{},
		originalComplete: true, costFactsComplete: true, displayComplete: true,
		modes: map[string]struct{}{},
	}
}

func (a *quoteAccumulator) add(quote CostQuote) {
	quote = NormalizeQuote(quote)
	if !quoteHasCompleteCostFact(quote) {
		a.costFactsComplete = false
		if a.out.IncompleteReason == "" {
			a.out.IncompleteReason = quote.IncompleteReason
		}
	}
	a.out.LegacyEstimate = a.out.LegacyEstimate || quote.LegacyEstimate
	if quote.BillingMode != "" {
		a.modes[quote.BillingMode] = struct{}{}
	}
	if quote.RateDate > a.out.RateDate {
		a.out.RateDate = quote.RateDate
	}
	originalCurrency := NormalizeCurrency(quote.Original.Currency)
	if len(quote.OriginalTotals) > 0 {
		for _, original := range quote.OriginalTotals {
			a.addOriginal(original, NormalizeCurrency(original.Currency))
		}
	} else {
		a.addOriginal(quote.Original, originalCurrency)
	}
	if a.display != "" {
		_, found := quote.Valuations[a.display]
		a.displayComplete = a.displayComplete && (found || originalCurrency == a.display)
	}
	originalIncluded := false
	for code, valuation := range quote.Valuations {
		code = NormalizeCurrency(code)
		if code == originalCurrency {
			originalIncluded = true
		}
		a.addValuation(code, valuation)
	}
	if originalCurrency != "" && !originalIncluded {
		a.addValuation(originalCurrency, Valuation{
			Money: quote.Original, Basis: BasisIdentity, Source: "aggregate",
			AsOf: time.Now().UTC().Format("2006-01-02"),
		})
	}
}

func (a *quoteAccumulator) addOriginal(original Money, currency string) {
	if currency != "" {
		a.originalTotals[currency] = a.originalTotals[currency].Add(original.AmountValue())
	}
	if a.originalCurrency == "" {
		a.originalCurrency = currency
		a.originalTotal = original.AmountValue()
		return
	}
	if currency != a.originalCurrency {
		a.originalComplete = false
		return
	}
	a.originalTotal = a.originalTotal.Add(original.AmountValue())
}

func (a *quoteAccumulator) addValuation(code string, valuation Valuation) {
	if code == "" {
		return
	}
	a.totals[code] = a.totals[code].Add(valuation.Money.AmountValue())
	current, found := a.out.Valuations[code]
	if !found {
		current = valuation
	}
	current.Money = MoneyOf(a.totals[code], code)
	current.Stale = current.Stale || valuation.Stale
	a.out.Valuations[code] = current
}

func (a *quoteAccumulator) finish() CostQuote {
	a.out.CostComplete = a.costFactsComplete
	if a.originalComplete && a.originalCurrency != "" {
		a.out.Original = MoneyOf(a.originalTotal, a.originalCurrency)
		a.syncOriginalValuation()
	} else {
		a.out.Original = Money{Amount: "0"}
	}
	if len(a.originalTotals) > 1 {
		codes := make([]string, 0, len(a.originalTotals))
		for code := range a.originalTotals {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes {
			a.out.OriginalTotals = append(a.out.OriginalTotals, MoneyOf(a.originalTotals[code], code))
		}
	}
	if len(a.modes) == 1 {
		for mode := range a.modes {
			a.out.BillingMode = mode
		}
	}
	if a.display == "" && a.originalComplete && a.costFactsComplete {
		selected := a.out.Original
		a.out.Selected = &selected
		a.out.DisplayComplete = true
		a.out.Complete = true
		a.out.DisplayStatus = DisplayStatusMatched
		a.out.AggregateMode = AggregateModeSingleCurrency
		return a.out
	}
	valuation, found := a.out.Valuations[a.display]
	if found && a.displayComplete && a.costFactsComplete {
		selected := valuation.Money
		a.out.Selected = &selected
		a.out.DisplayComplete = true
		a.out.Complete = true
		a.out.DisplayStatus = DisplayStatusMatched
		if len(a.originalTotals) > 1 {
			a.out.AggregateMode = AggregateModeCommonValuation
		} else {
			a.out.AggregateMode = AggregateModeSingleCurrency
		}
		return a.out
	}
	a.out.DisplayComplete = false
	a.out.Complete = false
	if !a.costFactsComplete {
		a.out.DisplayStatus = DisplayStatusUnavailable
		a.out.IncompleteReason = firstNonEmpty(a.out.IncompleteReason, "incomplete_cost_fact")
	} else if a.originalComplete && a.originalCurrency != "" {
		selected := a.out.Original
		a.out.Selected = &selected
		a.out.DisplayStatus = DisplayStatusFallbackOriginal
		a.out.AggregateMode = AggregateModeSingleCurrency
		a.out.IncompleteReason = firstNonEmpty(a.out.IncompleteReason, "display_unavailable")
	} else {
		a.out.DisplayStatus = DisplayStatusBucketed
		a.out.AggregateMode = AggregateModeCurrencyBuckets
		a.out.IncompleteReason = firstNonEmpty(a.out.IncompleteReason, "currency_buckets")
	}
	return a.out
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

func (a *quoteAccumulator) syncOriginalValuation() {
	valuation, found := a.out.Valuations[a.originalCurrency]
	if !found {
		valuation = Valuation{
			Basis: BasisIdentity, Source: "aggregate",
			AsOf: time.Now().UTC().Format("2006-01-02"),
		}
	}
	valuation.Money = a.out.Original
	a.out.Valuations[a.originalCurrency] = valuation
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
	return provider, model
}
