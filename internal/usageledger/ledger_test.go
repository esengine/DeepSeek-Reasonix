package usageledger

import (
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestProjectionAttributesModelsAndSumsExactUSDTicks(t *testing.T) {
	l := New()
	l.Add(event.Event{Kind: event.Usage, UsageSource: event.UsageSourcePlanner, UsageModel: "planner", Usage: &provider.Usage{PromptTokens: 3, CompletionTokens: 1, CacheMissTokens: 3}, Pricing: &provider.Pricing{Input: 0.1, Output: 0.2, Currency: "USD"}})
	l.Add(event.Event{Kind: event.Usage, UsageSource: event.UsageSourceExecutor, UsageModel: "executor", Usage: &provider.Usage{PromptTokens: 7, CompletionTokens: 2, CacheHitTokens: 7}, Pricing: &provider.Pricing{CacheHit: 0.01, Output: 0.2, Currency: "$"}})
	p := l.Projection()
	if p.Usage.InputTokens != 10 || p.Usage.OutputTokens != 3 || len(p.ModelUsage) != 2 {
		t.Fatalf("projection = %+v", p)
	}
	// USD 0.0000005 + USD 0.00000047 = 9700 ticks at 1e-10 USD/tick.
	if p.TotalCostUSDTicks == nil || *p.TotalCostUSDTicks != 9700 || p.TotalCostUSD == nil || *p.TotalCostUSD != 0.00000097 {
		t.Fatalf("cost projection = %+v", p)
	}
}

func TestProjectionFailsClosedForUnknownOrNonUSDPricing(t *testing.T) {
	for _, pricing := range []*provider.Pricing{nil, {Input: 1, Currency: "CNY"}} {
		l := New()
		l.Add(event.Event{Kind: event.Usage, UsageModel: "deepseek", Usage: &provider.Usage{PromptTokens: 100}, Pricing: pricing})
		p := l.Projection()
		if !p.CostIsPartial || p.TotalCostUSD != nil || p.TotalCostUSDTicks != nil {
			t.Fatalf("pricing %#v did not fail closed: %+v", pricing, p)
		}
	}
}

func TestProjectionMarksMissingAttributionAndOpenWorkIncomplete(t *testing.T) {
	l := New()
	l.Add(event.Event{Kind: event.Usage, Usage: &provider.Usage{PromptTokens: 1}, Pricing: &provider.Pricing{Input: 1, Currency: "USD"}})
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "task-1", Kind: "task", Status: "running"}})
	p := l.Projection()
	if !p.UsageIsIncomplete || p.OpenBackgroundSubagents != 1 || len(p.IncompleteReasons) != 2 {
		t.Fatalf("projection = %+v", p)
	}
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "task-1", Kind: "task", Status: "done"}})
	p = l.Projection()
	if !p.UsageIsIncomplete || p.OpenBackgroundSubagents != 0 || len(p.IncompleteReasons) != 1 {
		t.Fatalf("completed projection = %+v", p)
	}
}

func TestProjectionKeepsTimedOutBackgroundSubagentFailClosed(t *testing.T) {
	l := New()
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "task-1", Kind: "task", Status: "running"}})
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "task-1", Kind: "task", Status: "drain_timeout"}})
	p := l.Projection()
	if !p.UsageIsIncomplete || p.OpenBackgroundSubagents != 1 || len(p.IncompleteReasons) != 3 {
		t.Fatalf("timed-out projection = %+v", p)
	}
}

func TestProjectionIgnoresBackgroundShellLifecycle(t *testing.T) {
	l := New()
	l.Add(event.Event{Kind: event.BackgroundJobLifecycle, BackgroundJob: event.BackgroundJob{ID: "bash-1", Kind: "bash", Status: "running"}})
	p := l.Projection()
	if !p.UsageIsIncomplete || !p.CostIsPartial || p.OpenBackgroundSubagents != 0 || len(p.IncompleteReasons) != 1 || p.IncompleteReasons[0] != "usage_not_reported" {
		t.Fatalf("shell projection = %+v", p)
	}
}

func TestProjectionFailsClosedWhenProviderReportsNoUsage(t *testing.T) {
	p := New().Projection()
	if !p.UsageIsIncomplete || !p.CostIsPartial || p.TotalCostUSD != nil || p.TotalCostUSDTicks != nil {
		t.Fatalf("empty projection did not fail closed: %+v", p)
	}
	if len(p.IncompleteReasons) != 1 || p.IncompleteReasons[0] != "usage_not_reported" {
		t.Fatalf("incomplete reasons = %v", p.IncompleteReasons)
	}
}
