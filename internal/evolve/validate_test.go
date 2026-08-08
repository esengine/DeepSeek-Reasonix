package evolve

import "testing"

func sampleEvidence() []Evidence {
	return []Evidence{{
		SessionPath:  "/tmp/sessions/s1.jsonl",
		MessageIndex: 3,
		Kind:         "user_text",
		Quote:        "don't use force-push on main",
	}}
}

func baseProposal(tier Tier) Proposal {
	return Proposal{
		ID:         "evp_01",
		Status:     StatusProposed,
		Tier:       tier,
		Title:      "Avoid force-push on main",
		Why:        "User corrected repeated force-push attempts",
		HowToApply: "Never force-push main; open a PR instead",
		Evidence:   sampleEvidence(),
		Target: Target{
			Kind:        TargetMemory,
			MemoryType:  "feedback",
			MemoryScope: "project",
		},
	}
}

func TestValidateRequiresEvidence(t *testing.T) {
	p := baseProposal(TierL0)
	p.Evidence = nil
	if err := Validate(p); err == nil {
		t.Fatal("expected evidence error")
	}
}

func TestValidateRejectsSensitive(t *testing.T) {
	p := baseProposal(TierL0)
	p.HowToApply = "store api key: sk-abcdefghijklmnopqrstuvwxyz123456"
	if err := Validate(p); err == nil {
		t.Fatal("expected sensitive rejection")
	}
}

func TestValidateRejectsGlobalL0(t *testing.T) {
	p := baseProposal(TierL0)
	p.Target.MemoryScope = "global"
	if err := Validate(p); err == nil {
		t.Fatal("expected global scope rejection")
	}
}

func TestValidateRejectsOversizeL1(t *testing.T) {
	p := baseProposal(TierL1)
	p.Target.Kind = TargetAgentsMD
	p.Body = "- a\n- b\n- c\n- d\n- e\n- f"
	if err := Validate(p); err == nil {
		t.Fatal("expected L1 line budget error")
	}
}

func TestValidateAcceptsL0AndL1(t *testing.T) {
	if err := Validate(baseProposal(TierL0)); err != nil {
		t.Fatalf("L0: %v", err)
	}
	p1 := baseProposal(TierL1)
	p1.Target.Kind = TargetAgentsMD
	p1.Body = ""
	if err := Validate(p1); err != nil {
		t.Fatalf("L1: %v", err)
	}
}

func TestValidateAppliedIsOK(t *testing.T) {
	p := baseProposal(TierL0)
	p.Status = StatusApplied
	p.Evidence = nil
	if err := Validate(p); err != nil {
		t.Fatalf("applied should skip strict checks: %v", err)
	}
}

func TestValidateDiscardedFails(t *testing.T) {
	p := baseProposal(TierL0)
	p.Status = StatusDiscarded
	if err := Validate(p); err == nil {
		t.Fatal("expected discarded error")
	}
}
