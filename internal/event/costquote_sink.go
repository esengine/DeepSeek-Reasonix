package event

import (
	"strings"
	"sync"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/provider"
)

// QuoteContext supplies an explicit display request for the CostQuote
// middleware. Empty means auto and keeps the price-book currency.
type QuoteContext struct {
	mu              sync.RWMutex
	DisplayCurrency string
	DisplayRequest  billing.DisplayRequest
	// Now overrides the clock in tests.
	Now func() time.Time
	// BillingModeForModel resolves provider-owned billing semantics from the
	// immutable boot config. It keeps billing_mode authoritative even when a
	// custom provider name does not contain a token-plan heuristic.
	BillingModeForModel func(modelRef string) string
	// PricingContextForModel resolves trusted catalog/schedule identity from the
	// immutable boot config. It supersedes BillingModeForModel when present.
	PricingContextForModel func(modelRef string) billing.PricingContext
}

// SetDisplay updates the resolved display currency used for Selected.
func (c *QuoteContext) SetDisplay(currency string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.DisplayCurrency = billing.NormalizeCurrency(currency)
	c.DisplayRequest = billing.DisplayRequest{Currency: c.DisplayCurrency, Source: billing.DisplaySourceExplicit}
	c.mu.Unlock()
}

func (c *QuoteContext) SetDisplayRequest(request billing.DisplayRequest) {
	if c == nil {
		return
	}
	request.Currency = billing.NormalizeCurrency(request.Currency)
	if request.Source == "" {
		request.Source = billing.DisplaySourceAuto
	}
	c.mu.Lock()
	c.DisplayRequest = request
	c.DisplayCurrency = request.Currency
	c.mu.Unlock()
}

func (c *QuoteContext) snapshot() (request billing.DisplayRequest, now time.Time) {
	if c == nil {
		return billing.DisplayRequest{Source: billing.DisplaySourceAuto}, time.Now().UTC()
	}
	c.mu.RLock()
	request = c.DisplayRequest
	if request.Currency == "" && c.DisplayCurrency != "" {
		request.Currency = c.DisplayCurrency
	}
	if request.Source == "" {
		if request.Currency != "" {
			request.Source = billing.DisplaySourceExplicit
		} else {
			request.Source = billing.DisplaySourceAuto
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

func (c *QuoteContext) pricingContext(modelRef string) billing.PricingContext {
	if c == nil {
		return billing.PricingContext{}
	}
	c.mu.RLock()
	resolve := c.PricingContextForModel
	c.mu.RUnlock()
	if resolve != nil {
		return resolve(modelRef)
	}
	return billing.PricingContext{BillingMode: c.billingMode(modelRef)}
}

// CostQuoteSink fills CostQuote on Usage events before forwarding. Frontends
// must consume e.CostQuote and must not call Pricing.Cost for aggregation.
// The embedded forwarder carries the optional audit capabilities past this
// link: without it every recorder below a quoting sink — trajectory and stats
// among them — silently receives nothing.
type CostQuoteSink struct {
	AuditForwarder
	Ctx *QuoteContext
}

var _ OptionalSinkCapabilities = (*CostQuoteSink)(nil)

// NewCostQuoteSink wraps inner with quoting. A nil ctx still quotes the
// original price-book currency.
func NewCostQuoteSink(inner Sink, ctx *QuoteContext) *CostQuoteSink {
	if ctx == nil {
		ctx = &QuoteContext{}
	}
	return &CostQuoteSink{AuditForwarder: AuditForwarder{Inner: inner}, Ctx: ctx}
}

func (s *CostQuoteSink) Emit(e Event) {
	if s == nil {
		return
	}
	if e.Kind == Usage && e.Usage != nil && e.CostQuote == nil {
		e.CostQuote = EnsureCostQuote(e, s.Ctx)
	}
	if s.Inner != nil {
		s.Inner.Emit(e)
	}
}

// EnsureCostQuote builds a CostQuote for an event when missing.
func EnsureCostQuote(e Event, ctx *QuoteContext) *billing.CostQuote {
	if e.Usage == nil {
		return nil
	}
	display, now := ctx.snapshot()
	if e.Usage.Unknown {
		known := *e.Usage
		known.Unknown = false
		e.Usage = &known
		q := EnsureCostQuote(e, ctx)
		q.CostComplete, q.DisplayComplete, q.Complete = false, false, false
		q.Estimated, q.DisplayStatus, q.IncompleteReason = true, billing.DisplayStatusUnavailable, "usage_unknown"
		return q
	}
	if e.Pricing == nil {
		q := billing.CostQuote{
			Estimated: true, CostComplete: false, DisplayComplete: false, Complete: false,
			DisplayStatus: billing.DisplayStatusUnavailable, IncompleteReason: "no_price",
			ModelRef: e.ModelRef, UsageSource: e.UsageSource,
		}
		return &q
	}
	pricingCtx := ctx.pricingContext(e.ModelRef)
	mode := billing.BillingModePAYG
	if configured := strings.TrimSpace(pricingCtx.BillingMode); configured != "" {
		mode = configured
	} else if strings.Contains(strings.ToLower(e.ModelRef), "token-plan") ||
		strings.Contains(strings.ToLower(e.Source), "token-plan") {
		mode = billing.BillingModeSubscriptionEquivalent
	}
	card := rateCardFromPricing(e.Pricing)
	q := billing.BuildQuote(billing.QuoteInput{
		Usage:         usageTokens(e.Usage),
		Rates:         card,
		OccurredAt:    now,
		Display:       display,
		BillingMode:   mode,
		ModelRef:      e.ModelRef,
		UsageSource:   firstUsageSource(e),
		ProviderKind:  pricingCtx.ProviderKind,
		ModelID:       pricingCtx.ModelID,
		ScheduleID:    pricingCtx.ScheduleID,
		CatalogSource: pricingCtx.CatalogSource,
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

func rateCardFromPricing(p *provider.Pricing) billing.RateCard {
	if p == nil {
		return billing.RateCard{}
	}
	return billing.RateCard{
		CacheHit: p.CacheHit,
		Input:    p.Input,
		Output:   p.Output,
		Currency: billing.NormalizeCurrency(p.Currency),
	}
}

func usageTokens(u *provider.Usage) billing.UsageTokens {
	if u == nil {
		return billing.UsageTokens{}
	}
	return billing.UsageTokens{
		PromptTokens:           u.PromptTokens,
		CompletionTokens:       u.CompletionTokens,
		CacheHitTokens:         u.CacheHitTokens,
		CacheMissTokens:        u.CacheMissTokens,
		CacheWriteTokens:       u.CacheWriteTokens,
		CacheWriteBilledTokens: u.CacheWriteBilledTokens,
		Estimated:              u.Estimated,
	}
}
