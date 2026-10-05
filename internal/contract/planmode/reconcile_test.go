package planmode_test

import (
	"testing"

	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/tool"

	_ "reasonix/internal/tools/builtin"
)

// effectOf mirrors the agent's own derivation so this reconciliation measures
// the policy against the real tool set rather than a restatement of it.
func effectOf(readOnly bool) planmode.Effect {
	if readOnly {
		return planmode.EffectNone
	}
	return planmode.EffectSideEffect
}

func decideBuiltin(tl tool.Tool) (planmode.Decision, planmode.PlanSafety) {
	safety := planmode.PlanSafetyUnknown
	if classifier, ok := tl.(tool.PlanModeClassifier); ok {
		if classifier.PlanModeSafe() {
			safety = planmode.PlanSafetySafe
		} else {
			safety = planmode.PlanSafetyUnsafe
		}
	}
	return (planmode.Policy{}).Decide(planmode.Call{
		Name:     tl.Name(),
		ReadOnly: tl.ReadOnly(),
		Safety:   safety,
		Effect:   effectOf(tl.ReadOnly()),
	}), safety
}

// Every builtin lands on the side its own declarations put it: a reader plans,
// a writer waits for approval, and an explicit opt-out waits regardless. A new
// tool that fits none of those fails here rather than in a user's workspace.
func TestBuiltinPhaseClassifiersMatchPolicy(t *testing.T) {
	builtins := tool.Builtins()
	if len(builtins) == 0 {
		t.Fatal("tool.Builtins() is empty")
	}
	for _, tl := range builtins {
		got, safety := decideBuiltin(tl)
		want := safety == planmode.PlanSafetyUnsafe ||
			(safety != planmode.PlanSafetySafe && !tl.ReadOnly())
		if got.Blocked != want {
			t.Errorf("builtin %q readOnly=%v safety=%v blocked=%v, want %v: %s",
				tl.Name(), tl.ReadOnly(), safety, got.Blocked, want, got.Message)
		}
	}
}

// The anchors the rule above is meant to produce. Planning stays fully usable —
// reading, searching, tracking state, and asking the user all run — while the
// tools that change the workspace do not.
func TestPlanningPhaseAdmitsResearchAndRefusesWriters(t *testing.T) {
	planned := map[string]bool{
		"read_file": false, "grep": false, "glob": false, "ls": false,
		"todo_write": false, "web_fetch": false,
	}
	refused := map[string]bool{
		"write_file": false, "edit_file": false, "multi_edit": false,
		"move_file": false, "notebook_edit": false,
	}
	for _, tl := range tool.Builtins() {
		got, _ := decideBuiltin(tl)
		name := tl.Name()
		if _, ok := planned[name]; ok {
			planned[name] = true
			if got.Blocked {
				t.Errorf("planning tool %q was blocked: %s", name, got.Message)
			}
		}
		if _, ok := refused[name]; ok {
			refused[name] = true
			if !got.Blocked {
				t.Errorf("writer %q ran during planning", name)
			}
		}
	}
	for _, seen := range []map[string]bool{planned, refused} {
		for name, found := range seen {
			if !found {
				t.Errorf("builtin %q was not registered; the anchor no longer measures anything", name)
			}
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
