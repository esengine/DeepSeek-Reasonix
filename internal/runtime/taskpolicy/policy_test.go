package taskpolicy

import (
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/agentpreset"
)

func TestDeriveVerificationFollowsPreset(t *testing.T) {
	if p := Derive(Input{Preset: agentpreset.Balanced}); p.Verification != VerifyTargeted {
		t.Fatalf("balanced verification = %v, want targeted", p.Verification)
	}
	if p := Derive(Input{Preset: agentpreset.Delivery}); p.Verification != VerifyFull {
		t.Fatalf("delivery verification = %v, want full", p.Verification)
	}
}

// The host derives a turn's policy from structural signals only. Substring
// matching on user prose read "do not modify anything under tests/" as a
// blanket freeze and blocked the fix the same sentence asked for; limits the
// user wants belong to the permission system, which sees the real command.
func TestDeriveTakesNoUserProse(t *testing.T) {
	for _, f := range reflect.VisibleFields(reflect.TypeFor[Input]()) {
		switch f.Name {
		case "Preset", "PlanMode":
		default:
			t.Errorf("Input.%s: policy input must stay structural, never user text", f.Name)
		}
	}
}

// The plan signal is carried and rendered, never enforced: whether the phase
// admits a call is planmode's answer, and a second one here is what let the two
// drift until delegation fell through the gap between them.
func TestPlanModeSignalIsCarriedNotEnforced(t *testing.T) {
	p := Derive(Input{Preset: agentpreset.Delivery, PlanMode: true})
	if !p.PlanModeReadOnly {
		t.Fatal("plan mode must be recorded on the derived policy")
	}
	if !strings.Contains(ExecutionPolicyBlock(p), "constraint=plan-mode-read-only") {
		t.Fatal("the plan signal must reach the provider-visible policy block")
	}
	if Derive(Input{Preset: agentpreset.Delivery}).PlanModeReadOnly {
		t.Fatal("a non-plan turn must not carry the plan signal")
	}
}

func TestExecutionPolicyBlockStable(t *testing.T) {
	p := Derive(Input{Preset: agentpreset.Balanced})
	block := ExecutionPolicyBlock(p)
	if !strings.Contains(block, `preset="balanced"`) {
		t.Fatalf("block missing preset: %s", block)
	}
	if !strings.Contains(block, `version="3"`) {
		t.Fatalf("block missing version: %s", block)
	}
	if !strings.HasPrefix(block, "<execution-policy") || !strings.HasSuffix(block, "</execution-policy>") {
		t.Fatalf("bad block shape: %s", block)
	}
	if strings.Contains(block, "constraint=") {
		t.Fatalf("no constraint line belongs outside plan mode: %s", block)
	}
	if !strings.Contains(ExecutionPolicyBlock(Derive(Input{Preset: agentpreset.Balanced, PlanMode: true})), "constraint=plan-mode-read-only") {
		t.Fatal("plan mode must stay visible in the block")
	}
}
