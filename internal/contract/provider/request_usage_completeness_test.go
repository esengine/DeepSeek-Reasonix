package provider

import (
	"context"
	"testing"
)

func TestRequestOnlyUsageDoesNotCertifyTokenAccounting(t *testing.T) {
	ctx := WithRequestAttemptCounter(context.Background())
	if got := UsageWithRequestAttemptCount(ctx, nil); got != nil {
		t.Fatalf("unstarted request = %+v, want nil", got)
	}
	recordRequestAttempt(ctx)
	recordRequestAttempt(ctx)
	got := UsageWithRequestAttemptCount(ctx, nil)
	if got == nil || !got.Estimated || got.RequestCount != 2 || got.TotalTokens != 0 {
		t.Fatalf("request-only usage = %+v, want exact HTTP count without certified token accounting", got)
	}
	for _, original := range []Usage{{}, {PromptTokens: 1000, TotalTokens: 1000}, {Estimated: true, PromptTokens: 1000}} {
		got := UsageWithRequestAttemptCount(ctx, &original)
		if got == &original || got.Estimated != original.Estimated || got.PromptTokens != original.PromptTokens || got.RequestCount != 2 || original.RequestCount != 0 {
			t.Fatalf("copy = %+v original = %+v, want reported zero/known/estimated identity preserved without mutation", got, original)
		}
	}
}
