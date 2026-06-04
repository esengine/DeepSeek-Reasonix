package cli

import (
	"encoding/json"
	"os"

	"reasonix/internal/event"
)

// RunMetrics is the machine-readable token/cache/cost summary `run --metrics`
// writes, so a benchmark harness can read a run's cost without scraping stdout.
type RunMetrics struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheHitTokens   int     `json:"cache_hit_tokens"`
	CacheMissTokens  int     `json:"cache_miss_tokens"`
	Steps            int     `json:"steps"` // model calls (one per stream, incl. tool rounds)
	Cost             float64 `json:"cost"`
	Currency         string  `json:"currency"`
	Compactions      int     `json:"compactions"`
}

// metricsSink forwards every event to the real sink and accumulates the per-call
// Usage events into a RunMetrics. Cache totals are summed per call (not read from
// the cumulative SessionHit/Miss) so they match PromptTokens exactly.
type metricsSink struct {
	inner event.Sink
	m     RunMetrics
}

func (s *metricsSink) Emit(e event.Event) {
	if e.Kind == event.Usage && e.Usage != nil {
		u := e.Usage
		s.m.PromptTokens += u.PromptTokens
		s.m.CompletionTokens += u.CompletionTokens
		s.m.CacheHitTokens += u.CacheHitTokens
		s.m.CacheMissTokens += u.CacheMissTokens
		s.m.Steps++
		if p := e.Pricing; p != nil {
			s.m.Cost += (float64(u.CacheHitTokens)*p.CacheHit +
				float64(u.CacheMissTokens)*p.Input +
				float64(u.CompletionTokens)*p.Output) / 1e6
			s.m.Currency = p.Currency
		}
	}
	if e.Kind == event.CompactionStarted {
		s.m.Compactions++
	}
	s.inner.Emit(e)
}

func writeMetrics(path string, m RunMetrics) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
