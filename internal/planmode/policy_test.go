package planmode

import (
	"strings"
	"testing"
)

func TestDecideLeavesSafetyToPermissionsAndSandbox(t *testing.T) {
	p := Policy{
		AllowedTools:     []string{"legacy_reader"},
		ReadOnlyCommands: []string{"gh issue view"},
	}
	for _, call := range []Call{
		{Name: "read_file", ReadOnly: true},
		{Name: "write_file", ReadOnly: false},
		{Name: "bash", ReadOnly: false},
		{Name: "mcp__srv__query", ReadOnly: true, UntrustedReadOnly: true},
		{Name: "mcp__srv__write", ReadOnly: false},
		{Name: "self_reported_writer", ReadOnly: false, Safety: PlanSafetySafe},
	} {
		if got := p.Decide(call); got.Blocked {
			t.Errorf("ordinary call %q was phase-blocked: %s", call.Name, got.Message)
		}
	}
}

func TestDecideBlocksOnlyExplicitPhaseOptOut(t *testing.T) {
	got := (Policy{}).Decide(Call{Name: "complete_step", ReadOnly: true, Safety: PlanSafetyUnsafe})
	if !got.Blocked || !strings.Contains(got.Message, "only available after plan approval") {
		t.Fatalf("complete_step decision = %+v", got)
	}

	got = (Policy{}).Decide(Call{Name: "custom_phase_tool", Safety: PlanSafetyUnsafe})
	if !got.Blocked || !strings.Contains(got.Message, "planning workflow") {
		t.Fatalf("custom phase opt-out decision = %+v", got)
	}
}
