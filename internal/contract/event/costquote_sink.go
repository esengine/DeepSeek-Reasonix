package event

import (
	"reasonix/internal/contract/pricing"
	"strings"
	"sync"
	"time"

	"reasonix/internal/contract/provider"
)

// QuoteContext supplies an explicit display request for the CostQuote
// middleware. Empty means auto and keeps the price-book currency.
type QuoteContext struct {
	mu              sync.RWMutex
	DisplayCurrency string
	DisplayRequest  pricing.DisplayRequest
	// Now overrides the clock in tests.
	Now func() time.Time
	// BillingModeForModel resolves provider-owned billing semantics from the
	// immutable boot config. It keeps billing_mode authoritative even when a
	// custom provider name does not contain a token-plan heuristic.
	BillingModeForModel func(modelRef string) string
	// CatalogProviderForModel names the vendor whose official price table
	// applies, decided by the endpoint's host. Without it a quote has no
	// official table — which is the right answer for a relay.
	CatalogProviderForModel func(modelRef string) string
}

// SetDisplay updates the resolved display currency used for Selected.
func (c *QuoteContext) SetDisplay(currency string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.DisplayCurrency = pricing.NormalizeCurrency(currency)
	c.DisplayRequest = pricing.DisplayRequest{Currency: c.DisplayCurrency, Source: pricing.DisplaySourceExplicit}
	c.mu.Unlock()
}

func (c *QuoteContext) SetDisplayRequest(request pricing.DisplayRequest) {
	if c == nil {
		return
	}
	request.Currency = pricing.NormalizeCurrency(request.Currency)
	if request.Source == "" {
		request.Source = pricing.DisplaySourceAuto
	}
	c.mu.Lock()
	c.DisplayRequest = request
	c.DisplayCurrency = request.Currency
	c.mu.Unlock()
}

func (c *QuoteContext) snapshot() (request pricing.DisplayRequest, now time.Time) {
	if c == nil {
		return pricing.DisplayRequest{Source: pricing.DisplaySourceAuto}, time.Now().UTC()
	}
	c.mu.RLock()
	request = c.DisplayRequest
	if request.Currency == "" && c.DisplayCurrency != "" {
		request.Currency = c.DisplayCurrency
	}
	if request.Source == "" {
		if request.Currency != "" {
			request.Source = pricing.DisplaySourceExplicit
		} else {
			request.Source = pricing.DisplaySourceAuto
		}
	}
	nowFn := c.Now
	c.mu.RUnlock()
	if nowFn != nil {
		now = nowFn()
	} else {
		now = time.Now().UTC()
	}
	return request, now
}

func (c *QuoteContext) billingMode(modelRef string) string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	resolve := c.BillingModeForModel
	c.mu.RUnlock()
	if resolve == nil {
		return ""
	}
	return strings.TrimSpace(resolve(modelRef))
}

func (c *QuoteContext) catalogProvider(modelRef string) string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	resolve := c.CatalogProviderForModel
	c.mu.RUnlock()
	if resolve == nil {
		return ""
	}
	return strings.TrimSpace(resolve(modelRef))
}

// CostQuoteSink fills CostQuote on Usage events before forwarding. Frontends
// must consume e.CostQuote and must not call Pricing.Cost for aggregation.
type CostQuoteSink struct {
	AuditForwarder
	Inner Sink
	Ctx   *QuoteContext
}

// NewCostQuoteSink wraps inner with quoting. A nil ctx still quotes the
// original price-book currency.
func NewCostQuoteSink(inner Sink, ctx *QuoteContext) *CostQuoteSink {
	if ctx == nil {
		ctx = &QuoteContext{}
	}
	return &CostQuoteSink{AuditForwarder: AuditForwarder{Inner: inner}, Inner: inner, Ctx: ctx}
}

func (s *CostQuoteSink) Emit(e Event) {
	if s == nil {
		return
	}
	if e.Kind == Notice && e.Code == NoticeCodeDisplayCurrency {
		s.Ctx.SetDisplay(e.Detail)
	}
	if e.Kind == Usage && e.Usage != nil && e.CostQuote == nil {
		e.CostQuote = EnsureCostQuote(e, s.Ctx)
	}
	if s.Inner != nil {
		s.Inner.Emit(e)
	}
}

// EnsureCostQuote builds a CostQuote for an event when missing.
func EnsureCostQuote(e Event, ctx *QuoteContext) *pricing.CostQuote {
	if e.Usage == nil {
		return nil
	}
	display, now := ctx.snapshot()
	if e.Pricing == nil {
		q := pricing.CostQuote{
			Estimated: true, CostComplete: false, DisplayComplete: false, Complete: false,
			Coverage:      pricing.CoverageIncomplete,
			DisplayStatus: pricing.DisplayStatusUnavailable, IncompleteReason: "no_price",
			ModelRef: e.ModelRef, UsageSource: e.UsageSource,
		}
		return &q
	}
	mode := pricing.BillingModePAYG
	if configured := ctx.billingMode(e.ModelRef); configured != "" {
		mode = configured
	} else if strings.Contains(strings.ToLower(e.ModelRef), "token-plan") ||
		strings.Contains(strings.ToLower(e.Source), "token-plan") {
		mode = pricing.BillingModeSubscriptionEquivalent
	}
	card := rateCardFromPricing(e.Pricing)
	q := pricing.BuildQuote(pricing.QuoteInput{
		Usage:        usageTokens(e.Usage),
		Rates:        card,
		OccurredAt:   now,
		Display:      display,
		BillingMode:  mode,
		ModelRef:     e.ModelRef,
		UsageSource:  firstUsageSource(e),
		ProviderKind: ctx.catalogProvider(e.ModelRef),
	})
	return &q
}

func firstUsageSource(e Event) string {
	if s := strings.TrimSpace(e.UsageSource); s != "" {
		return s
	}
	if s := strings.TrimSpace(e.Source); s != "" {
		return s
	}
	return UsageSourceExecutor
}

func rateCardFromPricing(p *provider.Pricing) pricing.RateCard {
	if p == nil {
		return pricing.RateCard{}
	}
	return pricing.RateCard{
		CacheHit: p.CacheHit,
		Input:    p.Input,
		Output:   p.Output,
		Currency: pricing.NormalizeCurrency(p.Currency),
	}
}

func usageTokens(u *provider.Usage) pricing.UsageTokens {
	if u == nil {
		return pricing.UsageTokens{}
	}
	return pricing.UsageTokens{
		PromptTokens:           u.PromptTokens,
		CompletionTokens:       u.CompletionTokens,
		CacheHitTokens:         u.CacheHitTokens,
		CacheMissTokens:        u.CacheMissTokens,
		CacheWriteTokens:       u.CacheWriteTokens,
		CacheWriteBilledTokens: u.CacheWriteBilledTokens,
		Estimated:              u.Estimated,
	}
}
