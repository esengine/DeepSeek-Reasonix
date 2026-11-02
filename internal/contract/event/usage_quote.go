package event

import (
	"reasonix/internal/base/nilutil"
	"reasonix/internal/contract/pricing"
)

// UsageQuoter supplies the same occurrence-time quote used by event delivery.
type UsageQuoter interface {
	QuoteUsage(Event) *pricing.CostQuote
}

// QuoteUsage preserves an existing quote or asks the sink's pricing owner.
// Legacy sinks use only the event's supplied rate card, without a catalog guess.
func QuoteUsage(s Sink, e Event) *pricing.CostQuote {
	if e.CostQuote != nil {
		return e.CostQuote
	}
	if !nilutil.IsNil(s) {
		if q, ok := s.(UsageQuoter); ok {
			return q.QuoteUsage(e)
		}
	}
	return EnsureCostQuote(e, nil)
}

func (s *CostQuoteSink) QuoteUsage(e Event) *pricing.CostQuote {
	if e.CostQuote != nil {
		return e.CostQuote
	}
	if s == nil {
		return EnsureCostQuote(e, nil)
	}
	return EnsureCostQuote(e, s.Ctx)
}

func (f AuditForwarder) QuoteUsage(e Event) *pricing.CostQuote {
	return QuoteUsage(f.Inner, e)
}

func (s *syncSink) QuoteUsage(e Event) *pricing.CostQuote {
	s.mu.Lock()
	defer s.mu.Unlock()
	return QuoteUsage(s.inner, e)
}

func (c *coalescer) QuoteUsage(e Event) *pricing.CostQuote {
	return QuoteUsage(c.inner, e)
}
