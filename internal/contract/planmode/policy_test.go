package planmode

import (
	"strings"
	"testing"
)

// The phase barrier must never become a second opinion on danger: whatever a
// call's name or ReadOnly bool suggests, a caller that proved the call changes
// nothing outside the session gets through untouched.
func TestDecideLeavesToolSafetyToPermissionsAndSandbox(t *testing.T) {
	p := Policy{
		AllowedTools:     []string{"legacy_reader"},
		ReadOnlyCommands: []string{"gh issue view"},
	}
	for _, call := range []Call{
		{Name: "read_file", ReadOnly: true, Effect: EffectNone},
		{Name: "bash", ReadOnly: true, Effect: EffectNone},
		{Name: "mcp__srv__query", ReadOnly: true, Effect: EffectNone},
		{Name: "mcp__srv__write", ReadOnly: false, Effect: EffectNone},
		{Name: "self_reported_writer", ReadOnly: false, Safety: PlanSafetySafe},
	} {
		if got := p.Decide(call); got.Blocked {
			t.Errorf("call %q with no side effect was phase-blocked: %s", call.Name, got.Message)
		}
	}
}

// The phase's defining promise: nothing externally visible happens before the
// user approves a plan. Permissions cannot keep it — its modes are configured
// for the execution phase — so a refusal here says "still planning", and says so
// without claiming the caller lacks permission.
func TestDecideBlocksSideEffectsBeforeApproval(t *testing.T) {
	for _, call := range []Call{
		{Name: "write_file", ReadOnly: false, Effect: EffectSideEffect},
		{Name: "bash", ReadOnly: false, Effect: EffectSideEffect},
		{Name: "task", ReadOnly: false, Effect: EffectSideEffect},
		{Name: "remember", ReadOnly: false, Effect: EffectSideEffect},
	} {
		got := (Policy{}).Decide(call)
		if !got.Blocked {
			t.Fatalf("side-effecting %q ran during planning", call.Name)
		}
		if !strings.Contains(got.Message, "still planning") {
			t.Errorf("%q message does not name the phase: %s", call.Name, got.Message)
		}
		if strings.Contains(got.Message, "permission denied") || !strings.Contains(got.Message, "not a permission decision") {
			t.Errorf("%q message reads as a permission denial: %s", call.Name, got.Message)
		}
	}
}

// A caller that never answered gets the conservative reading. A phase barrier
// that fails open is the bug it exists to prevent.
func TestDecideTreatsUnclassifiedEffectAsSideEffect(t *testing.T) {
	got := (Policy{}).Decide(Call{Name: "opaque_plugin_tool"})
	if !got.Blocked {
		t.Fatal("an unclassified call was allowed to run during planning")
	}
}

func TestDecideBlocksExplicitPhaseOptOut(t *testing.T) {
	got := (Policy{}).Decide(Call{Name: "complete_step", ReadOnly: true, Effect: EffectNone, Safety: PlanSafetyUnsafe})
	if !got.Blocked || !strings.Contains(got.Message, "only available after plan approval") {
		t.Fatalf("complete_step decision = %+v", got)
	}

	got = (Policy{}).Decide(Call{Name: "custom_phase_tool", Safety: PlanSafetyUnsafe})
	if !got.Blocked || !strings.Contains(got.Message, "planning workflow") {
		t.Fatalf("custom phase opt-out decision = %+v", got)
	}
}

// Each refusal carries an identity a counter can group by. Collapsing them
// hides the one number worth watching: how often planning stops on classifier
// debt the host could go close, rather than on a real side effect.
func TestDecisionsCarryDistinguishableReasons(t *testing.T) {
	for _, tc := range []struct {
		call Call
		want BlockReason
	}{
		{Call{Name: "write_file", Effect: EffectSideEffect}, BlockSideEffect},
		{Call{Name: "opaque_plugin_tool"}, BlockUnclassified},
		{Call{Name: "complete_step", Safety: PlanSafetyUnsafe}, BlockPhaseOptOut},
		{Call{Name: "custom_phase_tool", Safety: PlanSafetyUnsafe}, BlockPhaseOptOut},
	} {
		got := (Policy{}).Decide(tc.call)
		if !got.Blocked || got.Reason != tc.want {
			t.Errorf("%q reason = %q, want %q (blocked=%v)", tc.call.Name, got.Reason, tc.want, got.Blocked)
		}
	}
	if got := (Policy{}).Decide(Call{Name: "read_file", Effect: EffectNone}); got.Reason != "" {
		t.Errorf("an admitted call carries reason %q", got.Reason)
	}
}
