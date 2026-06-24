package usage

import (
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// CalcCost computes the cost for a set of token counts given pricing info.
// Returns cost amount and currency string.
func CalcCost(cacheHit, cacheMiss, completion int, p *provider.Pricing) (float64, string) {
	if p == nil {
		return 0, ""
	}
	cost := (float64(cacheHit)*p.CacheHit +
		float64(cacheMiss)*p.Input +
		float64(completion)*p.Output) / 1e6
	return cost, p.Currency
}

// Sink is a decorator event.Sink that intercepts event.Usage events,
// buffers them in a channel, and batch-writes them to per-day JSONL files via a
// background goroutine. All other event kinds are forwarded to the inner sink.
//
// The channel is non-blocking: if the buffer (256) is full, records are silently
// dropped rather than blocking the agent's event pipeline.
type Sink struct {
	inner event.Sink
	store *Store
}

// NewSink creates a Sink backed by the given Store.
func NewSink(inner event.Sink, store *Store) *Sink {
	return &Sink{inner: inner, store: store}
}

// Emit intercepts event.Usage, converts it to a Record, and submits it to the
// Store's buffered channel. All events (including Usage) are forwarded to the
// inner sink so downstream consumers continue to work.
func (s *Sink) Emit(e event.Event) {
	if e.Kind == event.Usage && e.Usage != nil {
		r := Record{
			TS:               time.Now(),
			Provider:         e.ProviderName,
			Model:            e.ModelName,
			UsageSource:      e.UsageSource,
			PromptTokens:     e.Usage.PromptTokens,
			CompletionTokens: e.Usage.CompletionTokens,
			CacheHitTokens:   e.Usage.CacheHitTokens,
			CacheMissTokens:  e.Usage.CacheMissTokens,
			ReasoningTokens:  e.Usage.ReasoningTokens,
			TotalTokens:      e.Usage.TotalTokens,
			FinishReason:     e.Usage.FinishReason,
			LatencyMS:        e.LatencyMS,
			SessionID:        e.SessionID,
		}
		if p := e.Pricing; p != nil {
			r.Cost, r.Currency = CalcCost(r.CacheHitTokens, r.CacheMissTokens, r.CompletionTokens, p)
		}
		s.store.Write(r)
	}
	if s.inner != nil {
		s.inner.Emit(e)
	}
}

// Close closes the underlying Store (flushes + closes files).
func (s *Sink) Close() {
	s.store.Close()
}
