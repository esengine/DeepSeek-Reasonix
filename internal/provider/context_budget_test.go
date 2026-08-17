package provider

import (
	"context"
	"testing"
)

type legacySharedProvider struct {
	budget int
	shared bool
}

func (legacySharedProvider) Name() string { return "legacy" }
func (legacySharedProvider) Stream(context.Context, Request) (<-chan Chunk, error) {
	return nil, nil
}
func (p legacySharedProvider) OutputBudget() int         { return p.budget }
func (p legacySharedProvider) SharesContextWindow() bool { return p.shared }

type policyProvider struct {
	policy ContextBudgetPolicy
}

func (policyProvider) Name() string { return "policy" }
func (policyProvider) Stream(context.Context, Request) (<-chan Chunk, error) {
	return nil, nil
}
func (p policyProvider) ContextBudgetPolicy() ContextBudgetPolicy { return p.policy }

func TestResolveContextBudgetPolicyFallsBackToLegacyInterfaces(t *testing.T) {
	got := ResolveContextBudgetPolicy(legacySharedProvider{budget: 128_000, shared: true})
	if got.WindowMode != ContextWindowShared || got.AutoOutputTokens != 128_000 || got.LimitMode != OutputLimitAlways {
		t.Fatalf("legacy shared policy = %+v", got)
	}
	unknown := ResolveContextBudgetPolicy(legacySharedProvider{budget: 0, shared: false})
	if unknown.WindowMode != ContextWindowUnknown || unknown.AutoOutputTokens != 0 {
		t.Fatalf("legacy unknown policy = %+v", unknown)
	}
}

func TestResolveContextBudgetPolicyPrefersNewInterface(t *testing.T) {
	want := ContextBudgetPolicy{
		WindowMode:       ContextWindowShared,
		AutoOutputTokens: DeepSeekMaxOutputTokens,
		MaxOutputTokens:  DeepSeekMaxOutputTokens,
		LimitMode:        OutputLimitOmitWhenSafe,
	}
	got := ResolveContextBudgetPolicy(policyProvider{policy: want})
	if got != want {
		t.Fatalf("policy = %+v, want %+v", got, want)
	}
}

func TestResolveContextBudgetPolicyNilProvider(t *testing.T) {
	if got := ResolveContextBudgetPolicy(nil); got.WindowMode != ContextWindowUnknown {
		t.Fatalf("nil policy = %+v", got)
	}
}
