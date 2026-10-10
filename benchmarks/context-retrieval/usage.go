package main

import (
	"fmt"
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// usageSink reads what a run cost from the events the agent already emits. Every
// billed completion reports event.Usage — the round path, compaction, command
// routing and failure diagnosis all do — so the bench reads the same record the
// product reads instead of wrapping a provider and counting a second time.
type usageSink struct {
	mu       sync.Mutex
	requests int
	usage    provider.Usage
}

func (s *usageSink) Emit(e event.Event) {
	if e.Kind != event.Usage || e.Usage == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// One event is one completion that reported usage; RequestCount covers a
	// completion that merged attempts. A request that failed before reporting
	// leaves no event, so this is not a count of attempts.
	if e.Usage.RequestCount > 0 {
		s.requests += e.Usage.RequestCount
	} else {
		s.requests++
	}
	u := *e.Usage
	s.usage.PromptTokens += u.PromptTokens
	s.usage.CompletionTokens += u.CompletionTokens
	s.usage.TotalTokens += u.TotalTokens
	s.usage.CacheHitTokens += u.CacheHitTokens
	s.usage.CacheMissTokens += u.CacheMissTokens
	s.usage.ReasoningTokens += u.ReasoningTokens
	s.usage.Estimated = s.usage.Estimated || u.Estimated
}

func (s *usageSink) snapshot() (provider.Usage, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage, s.requests
}

// promptTokenSuffix renders what a run cost only when something reported it. A
// dry run reports nothing, and a trailing "prompt-tok=0" would read as a
// measured zero rather than as no measurement.
func promptTokenSuffix(m contextMetrics) string {
	if m.UsageReportedRequests == 0 {
		return ""
	}
	return fmt.Sprintf(" prompt-tok=%d", m.PromptTokens)
}
