package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestMaybeCompactIgnoresRetryAggregateBelowContextThreshold(t *testing.T) {
	// compact_ratio is the sole trigger; LatestPromptTokens (context attempt)
	// must drive the decision, not the billable retry aggregate.
	const window = 10_000
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("large earlier request ", 500)},
		{Role: provider.RoleUser, Content: "current request"},
		{Role: provider.RoleAssistant, Content: "current answer"},
	}}
	a := New(&fakeProvider{reply: "summary"}, tool.NewRegistry(), sess, Options{
		ContextWindow: window,
		CompactRatio:  0.85,
		RecentKeep:    2,
	}, event.Discard)

	// Aggregate 9000 would be above fold (8500); latest context attempt 4000 is not.
	prepareForObservedUsage(a, context.Background(), &provider.Usage{
		PromptTokens:        9000,
		ContextPromptTokens: 4000,
	})

	if hasCompactionSummary(visibleContext(a)) {
		t.Fatal("retry-aggregate prompt tokens triggered compaction below the latest context threshold")
	}
}

func TestMaybeCompactStillTriggersAtLatestContextThreshold(t *testing.T) {
	const window = 10_000
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("large earlier request ", 500)},
		{Role: provider.RoleUser, Content: "current request"},
		{Role: provider.RoleAssistant, Content: "current answer"},
	}}
	a := New(&fakeProvider{reply: "summary"}, tool.NewRegistry(), sess, Options{
		ContextWindow: window,
		CompactRatio:  0.85,
		RecentKeep:    2,
	}, event.Discard)

	// Latest context attempt at/above fold trigger (8500) must install a summary.
	prepareForObservedUsage(a, context.Background(), &provider.Usage{
		PromptTokens:        16000,
		ContextPromptTokens: 8600,
	})

	if !hasCompactionSummary(visibleContext(a)) {
		t.Fatal("latest prompt at the configured threshold did not trigger compaction")
	}
}

func TestMissingReasoningRetryAggregateDoesNotTriggerEarlyCompaction(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}},
			Usage:     &provider.Usage{PromptTokens: 4000, CompletionTokens: 2, TotalTokens: 4002, CacheMissTokens: 4000, FinishReason: "tool_calls"},
		},
		testutil.Turn{
			Reasoning: "retry reasoning",
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}},
			Usage:     &provider.Usage{PromptTokens: 4000, CompletionTokens: 3, TotalTokens: 4003, CacheHitTokens: 4000, ReasoningTokens: 2, FinishReason: "tool_calls"},
		},
		testutil.Turn{
			Text:  "done",
			Usage: &provider.Usage{PromptTokens: 4100, CompletionTokens: 1, TotalTokens: 4101, CacheHitTokens: 4000, CacheMissTokens: 100},
		},
	)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "original task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old analysis ", 100)},
		{Role: provider.RoleUser, Content: "follow-up"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("more old work ", 100)},
	}}
	sink := &recordSink{}
	a := New(strictToolCallReasoningProvider{mp}, echoRegistry(), sess, Options{
		ContextWindow: 10_000,
		ArchiveDir:    t.TempDir(),
	}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(sink.kinds(event.CompactionStarted)); got != 0 {
		t.Fatalf("compactions = %d, want none while latest prompt is below threshold", got)
	}
	usageEvents := sink.kinds(event.Usage)
	if len(usageEvents) == 0 || usageEvents[0].Usage == nil {
		t.Fatalf("missing recovery usage event: %+v", usageEvents)
	}
	if got := usageEvents[0].Usage; got.PromptTokens != 8000 || got.ContextPromptTokens != 4000 {
		t.Fatalf("recovery usage = %+v, want aggregate prompt 8000 and latest context prompt 4000", got)
	}
}
