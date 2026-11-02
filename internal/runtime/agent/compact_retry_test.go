package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestMaybeCompactIgnoresRetryAggregateBelowContextThreshold(t *testing.T) {
	// compact_ratio is the sole trigger; LatestPromptTokens (context attempt)
	// must drive the decision, not the billable retry aggregate.
	const window = 10_000
	sess := &sessionstore.Session{Messages: []provider.Message{
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
	sess := &sessionstore.Session{Messages: []provider.Message{
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
