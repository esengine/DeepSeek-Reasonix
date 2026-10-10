package main

import (
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// The cost a run reports must be what the agent reported, summed across every
// completion it billed — including one that merged attempts and one that could
// only estimate. Any other event kind is not a cost.
func TestUsageSinkAccumulatesWhatTheAgentReports(t *testing.T) {
	var sink usageSink
	sink.Emit(event.Event{}) // a kind that is not Usage
	sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110, CacheHitTokens: 60, CacheMissTokens: 40,
	}})
	sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 7, CompletionTokens: 3, CacheHitTokens: 2, CacheMissTokens: 5, RequestCount: 2, Estimated: true,
	}})
	sink.Emit(event.Event{Kind: event.Usage}) // a Usage event with no payload is not a cost

	usage, requests := sink.snapshot()
	if requests != 3 {
		t.Fatalf("requests = %d, want 3 (1 + the merged attempt's 2; the empty event adds none)", requests)
	}
	if usage.PromptTokens != 107 || usage.CompletionTokens != 13 || usage.TotalTokens != 110 {
		t.Fatalf("accumulated usage = %+v", usage)
	}
	if usage.CacheHitTokens != 62 || usage.CacheMissTokens != 45 {
		t.Fatalf("cache split = %+v", usage)
	}
	if !usage.Estimated {
		t.Fatal("Estimated was dropped when one completion reported an estimate")
	}
}

// A dry run must not claim a cost it never paid, in the JSON or in the line a
// person reads: an empty suffix, not "prompt-tok=0".
func TestUsageSinkStaysEmptyWithoutUsage(t *testing.T) {
	var sink usageSink
	sink.Emit(event.Event{})
	usage, requests := sink.snapshot()
	if requests != 0 || usage.PromptTokens != 0 || usage.CompletionTokens != 0 {
		t.Fatalf("nothing reported usage, got %+v requests=%d", usage, requests)
	}
	if suffix := promptTokenSuffix(contextMetrics{}); suffix != "" {
		t.Fatalf("a run with no reported usage still printed %q", suffix)
	}
	if suffix := promptTokenSuffix(contextMetrics{UsageReportedRequests: 1, PromptTokens: 12}); suffix != " prompt-tok=12" {
		t.Fatalf("a reported cost printed %q", suffix)
	}
}
