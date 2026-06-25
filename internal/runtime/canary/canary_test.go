package canary

import (
	"strings"
	"testing"
)

func TestEvaluateModesAreDeterministic(t *testing.T) {
	if got := Evaluate(Policy{Mode: SafeMode}, "same-key"); got.Enabled {
		t.Fatalf("safe mode enabled execution: %+v", got)
	}
	if got := Evaluate(Policy{Mode: FullProductionMode}, "same-key"); !got.Enabled || got.Mode != FullProductionMode {
		t.Fatalf("full production did not enable execution: %+v", got)
	}
	policy := Policy{Mode: CanaryMode, TrafficPercent: 10}
	first := Evaluate(policy, "same-key")
	for i := 0; i < 5; i++ {
		if got := Evaluate(policy, "same-key"); got.Enabled != first.Enabled || got.Mode != first.Mode {
			t.Fatalf("canary evaluation is not deterministic: first=%+v got=%+v", first, got)
		}
	}
}

func TestPromoteRequiresStableRunsAndStagesRollout(t *testing.T) {
	policy := Policy{Mode: CanaryMode, TrafficPercent: 10, MinStableRuns: 3}
	if got := Promote(policy, 2, 1); got.TrafficPercent != 10 || got.Mode != CanaryMode {
		t.Fatalf("promotion ignored stable run floor: %+v", got)
	}
	got := Promote(policy, 3, 0.9)
	if got.Mode != CanaryMode || got.TrafficPercent != 25 {
		t.Fatalf("canary should stage to 25%%, got %+v", got)
	}
	got = Promote(Policy{Mode: CanaryMode, TrafficPercent: 100, MinStableRuns: 3}, 3, 0.9)
	if got.Mode != FullProductionMode {
		t.Fatalf("100%% stable canary should promote to full production: %+v", got)
	}
}

func TestCompareBehaviorDetectsDecisionLevelDivergence(t *testing.T) {
	diff := CompareBehavior(
		BehaviorSample{Decision: "production_hardening", Strategy: "bugfix", Outcome: "success", Steps: []string{"a", "b"}},
		BehaviorSample{Decision: "production_hardening", Strategy: "review", Outcome: "success", Steps: []string{"a", "c"}},
	)
	if !diff.Diverged {
		t.Fatalf("expected behavior divergence: %+v", diff)
	}
	got := strings.Join(diff.Reasons, "\n")
	for _, want := range []string{"strategy diverged", "execution steps diverged"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in reasons: %+v", want, diff.Reasons)
		}
	}
	if diff.Attribution.PrimaryCause == "" || len(diff.Attribution.Factors) == 0 {
		t.Fatalf("missing causal attribution: %+v", diff.Attribution)
	}
	if diff.Attribution.PrimaryCause != "strategy_changed" {
		t.Fatalf("primary cause = %q, want strategy_changed", diff.Attribution.PrimaryCause)
	}
}

func TestCompareBehaviorExplainsDecisionAndOutcomeDivergence(t *testing.T) {
	diff := CompareBehavior(
		BehaviorSample{Decision: "production_hardening", Strategy: "bugfix", Outcome: "partial_success", Steps: []string{"a"}, DecisionReasons: []string{"budget blocked"}},
		BehaviorSample{Decision: "baseline", Strategy: "bugfix", Outcome: "success", Steps: []string{"a"}},
	)
	if !diff.Diverged {
		t.Fatalf("expected divergence: %+v", diff)
	}
	if diff.Attribution.PrimaryCause != "decision_changed" {
		t.Fatalf("primary cause = %q, want decision_changed", diff.Attribution.PrimaryCause)
	}
	got := ""
	for _, factor := range diff.Attribution.Factors {
		got += factor.Layer + ":" + factor.Cause + "\n"
	}
	for _, want := range []string{"control:decision_changed", "execution:outcome_changed", "runtime:decision_reason"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing attribution %q in %+v", want, diff.Attribution.Factors)
		}
	}
}
