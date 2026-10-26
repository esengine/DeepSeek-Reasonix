package provider

import (
	"context"
	"errors"
	"testing"
)

func TestAuxiliaryUsageObserverReplacesAndCopies(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	outer, inner := 0, 0
	ctx = WithAuxiliaryUsageObserver(ctx, func(*Usage) { outer++ })
	ctx = WithAuxiliaryUsageObserver(ctx, func(u *Usage) { inner++; u.PromptTokens = 999; u.Estimated = true })
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("observer changed cancellation")
	}
	u := Usage{PromptTokens: 10, CompletionTokens: 2, RequestCount: 3}
	ObserveAuxiliaryUsage(ctx, &u)
	if outer != 0 || inner != 1 || u.PromptTokens != 10 || u.Estimated {
		t.Fatalf("observer leaked or duplicated:outer%d inner%d usage%+v", outer, inner, u)
	}
	ObserveAuxiliaryUsage(ctx, nil)
	ObserveAuxiliaryUsage(context.Background(), &u)
	ObserveAuxiliaryUsage(WithAuxiliaryUsageObserver(ctx, nil), &u)
	if inner != 1 {
		t.Fatal("unknown usage or disabled observer caused a charge")
	}
}
