package planmode

import (
	"strings"
	"testing"
)

// The marker tells the model what the host will and will not run while
// planning. Both halves of that promise are checked against the policy that
// keeps it: a marker that overstates the barrier teaches the model to retry
// into a wall, and one that understates it teaches the model to skip the plan.
func TestMarkerPromisesMatchWhatThePolicyEnforces(t *testing.T) {
	for _, want := range []string{
		"planning workflow",
		"Do not begin implementation",
		"the host refuses",
		"not a permission decision",
		"Permissions and Sandbox",
		"approve the plan before the workflow switches to implementation",
	} {
		if !strings.Contains(Marker, want) {
			t.Fatalf("Marker missing %q: %s", want, Marker)
		}
	}

	for _, call := range []Call{
		{Name: "write_file", Effect: EffectSideEffect},
		{Name: "bash", Effect: EffectSideEffect},
		{Name: "task", Effect: EffectSideEffect},
	} {
		if got := (Policy{}).Decide(call); !got.Blocked {
			t.Fatalf("Marker says the host refuses %q while planning: %+v", call.Name, got)
		}
	}

	for _, call := range []Call{
		{Name: "read_file", Effect: EffectNone},
		{Name: "bash", Effect: EffectNone},
		{Name: "todo_write", Effect: EffectNone},
	} {
		if got := (Policy{}).Decide(call); got.Blocked {
			t.Fatalf("Marker says %q stays available while planning: %+v", call.Name, got)
		}
	}
}

func TestMarkerPhaseOptOutMatchesPolicy(t *testing.T) {
	if got := (Policy{}).Decide(Call{Name: "complete_step", Safety: PlanSafetyUnsafe}); !got.Blocked {
		t.Fatal("complete_step phase opt-out must remain enforced")
	}
}

// A superseded text that drifted back into being the current one would strip
// twice and hide a real prefix; an empty list means replayed sessions render
// the marker as if the user had typed it.
func TestSupersededMarkersAreHistoricalAndDistinct(t *testing.T) {
	if len(Superseded) == 0 {
		t.Fatal("Superseded is empty; earlier sessions can no longer strip their marker")
	}
	seen := map[string]bool{}
	for i, s := range Superseded {
		if s == Marker {
			t.Errorf("Superseded[%d] is the current Marker", i)
		}
		if !strings.HasPrefix(s, "[Plan mode") {
			t.Errorf("Superseded[%d] is not a plan marker: %.40s", i, s)
		}
		if seen[s] {
			t.Errorf("Superseded[%d] is a duplicate", i)
		}
		seen[s] = true
	}
}
