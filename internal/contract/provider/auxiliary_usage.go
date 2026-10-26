package provider

import "context"

type auxiliaryUsageObserverKey struct{}

// WithAuxiliaryUsageObserver binds one trusted host accounting observer. It
// replaces an inherited observer so nested scopes do not charge a root twice.
// A nil observer disables observation. Cancellation and deadlines are unchanged.
func WithAuxiliaryUsageObserver(ctx context.Context, observe func(*Usage)) context.Context {
	return context.WithValue(ctx, auxiliaryUsageObserverKey{}, observe)
}

// ObserveAuxiliaryUsage reports one logical call's finalized usage without
// exposing its caller-owned record to mutation. Observers may run concurrently
// and own their synchronization. No observer or unknown usage is a no-op.
func ObserveAuxiliaryUsage(ctx context.Context, usage *Usage) {
	if usage == nil {
		return
	}
	observe, _ := ctx.Value(auxiliaryUsageObserverKey{}).(func(*Usage))
	if observe != nil {
		copy := *usage
		observe(&copy)
	}
}
