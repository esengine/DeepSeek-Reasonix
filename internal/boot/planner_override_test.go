package boot

import (
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/config"
)

func TestPlannerOffHasHighestPrecedence(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agent.PlannerModel = "configured-planner"
	if got := effectivePlannerModel(cfg, Options{}); got != "configured-planner" {
		t.Fatalf("ordinary planner model = %q", got)
	}
	off := Options{Ablation: ablation.New(ablation.Planner)}
	if got := effectivePlannerModel(cfg, off); got != "" {
		t.Fatalf("planner-off returned %q, want disabled", got)
	}
	other := Options{Ablation: ablation.New(ablation.Evidence)}
	if got := effectivePlannerModel(cfg, other); got != "configured-planner" {
		t.Fatalf("an unrelated ablation disabled the planner: %q", got)
	}
}
