package planmode

import (
	"strings"
	"testing"
)

func TestMarkerStatesWorkflowAndPermissionBoundariesSeparately(t *testing.T) {
	for _, want := range []string{
		"planning workflow",
		"Do not begin implementation",
		"not a permission boundary",
		"Permissions and Sandbox",
		"approve the plan before the workflow switches to implementation",
	} {
		if !strings.Contains(Marker, want) {
			t.Fatalf("Marker missing %q: %s", want, Marker)
		}
	}

	for _, call := range []Call{
		{Name: "write_file"},
		{Name: "bash"},
		{Name: "task"},
	} {
		if got := (Policy{}).Decide(call); got.Blocked {
			t.Fatalf("Marker guidance must not become a security gate for %q: %+v", call.Name, got)
		}
	}
}

func TestMarkerPhaseOptOutMatchesPolicy(t *testing.T) {
	if got := (Policy{}).Decide(Call{Name: "complete_step", Safety: PlanSafetyUnsafe}); !got.Blocked {
		t.Fatal("complete_step phase opt-out must remain enforced")
	}
}
