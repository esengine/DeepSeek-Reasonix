package planmode_test

import (
	"testing"

	"reasonix/internal/planmode"
	"reasonix/internal/tool"

	_ "reasonix/internal/tool/builtin"
)

func TestBuiltinPhaseClassifiersMatchPolicy(t *testing.T) {
	builtins := tool.Builtins()
	if len(builtins) == 0 {
		t.Fatal("tool.Builtins() is empty")
	}
	for _, tl := range builtins {
		safety := planmode.PlanSafetyUnknown
		if classifier, ok := tl.(tool.PlanModeClassifier); ok {
			if classifier.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			} else {
				safety = planmode.PlanSafetyUnsafe
			}
		}
		got := (planmode.Policy{}).Decide(planmode.Call{
			Name:     tl.Name(),
			ReadOnly: tl.ReadOnly(),
			Safety:   safety,
		})
		if got.Blocked != (safety == planmode.PlanSafetyUnsafe) {
			t.Errorf("builtin %q safety=%v decision=%+v", tl.Name(), safety, got)
		}
	}
}

func TestCompleteStepExplicitlyOptsOutOfPlanPhase(t *testing.T) {
	for _, tl := range tool.Builtins() {
		if tl.Name() != "complete_step" {
			continue
		}
		classifier, ok := tl.(tool.PlanModeClassifier)
		if !ok || classifier.PlanModeSafe() {
			t.Fatal("complete_step must explicitly opt out of the planning phase")
		}
		return
	}
	t.Fatal("complete_step builtin not registered")
}
