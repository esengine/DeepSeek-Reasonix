package taskcontract

import (
	"encoding/json"
	"testing"

	"reasonix/internal/evidence"
)

func deliveryWriteReceipt(t *testing.T, path string, floor PolicyFloor) evidence.Receipt {
	t.Helper()
	return evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		Args: json.RawMessage(`{"path":"` + path + `"}`), Paths: []string{path},
		PolicyFloor: floor.String(),
	}
}

func fullVerifyReceipt(command string) evidence.Receipt {
	return evidence.Receipt{
		ToolName: "bash", Success: true, Command: command,
		Verification: evidence.VerificationPassed,
	}
}

func hasObligationKind(c *Contract, kind ObligationKind, enf Enforcement, origin ReasonCode) bool {
	t := false
	_ = t
	for _, o := range c.Obligations {
		if o.Kind == kind && o.Enforcement == enf && (origin == "" || o.Origin == origin) {
			return true
		}
	}
	return false
}

func TestDeliveryFloorWriteAddsStrictFullVerify(t *testing.T) {
	c := New("")
	c.AbsorbReceipt(1, deliveryWriteReceipt(t, "internal/agent/agent.go", PolicyFloorDelivery), "", false, false)
	if !hasObligationKind(c, ObligationFullVerify, EnforcementStrict, ReasonPolicyFloor) {
		t.Fatalf("delivery floor write missing strict full-verify obligation: %+v", c.Obligations)
	}
}

func TestDeliveryScratchWriteAddsNoFloorObligation(t *testing.T) {
	c := New("")
	c.AbsorbReceipt(1, deliveryWriteReceipt(t, "/tmp/btc_klines.py", PolicyFloorDelivery), "", false, false)
	if hasObligationKind(c, ObligationFullVerify, 0, ReasonPolicyFloor) {
		t.Fatalf("delivery scratch write must not create a floor obligation: %+v", c.Obligations)
	}
	if hasObligationKind(c, ObligationDiffReview, 0, "") {
		t.Fatalf("delivery scratch write must not create a diff review: %+v", c.Obligations)
	}
}

func TestStandardWriteAddsNoFloorObligation(t *testing.T) {
	c := New("")
	c.AbsorbReceipt(1, deliveryWriteReceipt(t, "internal/agent/agent.go", PolicyFloorNone), "", false, false)
	if hasObligationKind(c, ObligationFullVerify, 0, ReasonPolicyFloor) {
		t.Fatalf("standard write must not create a floor obligation: %+v", c.Obligations)
	}
}

func TestDeliveryFloorClearsOnFullVerification(t *testing.T) {
	c := New("")
	c.AbsorbReceipt(1, deliveryWriteReceipt(t, "internal/agent/agent.go", PolicyFloorDelivery), "", false, false)
	c.AbsorbReceipt(2, fullVerifyReceipt("go test ./..."), "", false, false)
	if hasUnsatisfiedFloorObligation(c) {
		t.Fatalf("full verification must clear the floor obligation: %+v", c.Obligations)
	}
}

func TestDeliveryFloorTargetedVerificationDoesNotClear(t *testing.T) {
	c := New("")
	c.AbsorbReceipt(1, deliveryWriteReceipt(t, "internal/agent/agent.go", PolicyFloorDelivery), "", false, false)
	c.AbsorbReceipt(2, fullVerifyReceipt("go test ./internal/agent"), "", false, false)
	if !hasUnsatisfiedFloorObligation(c) {
		t.Fatalf("targeted verification must not clear the floor obligation: %+v", c.Obligations)
	}
}

// The ratchet contract under pure replay: a delivery-floor write keeps its
// strict obligation after the session floor drops, and a later standard write
// gains none — the stamp, not the current floor, decides.
func TestFloorReplaySurvivesSessionDowngrade(t *testing.T) {
	receipts := []evidence.Receipt{
		deliveryWriteReceipt(t, "internal/agent/agent.go", PolicyFloorDelivery),
		deliveryWriteReceipt(t, "internal/cli/cli.go", PolicyFloorNone),
	}
	rebuilt := Rebuild(RebuildFacts{Receipts: receipts})
	if !hasUnsatisfiedFloorObligation(rebuilt) {
		t.Fatalf("downgraded session must keep the delivery write's floor obligation: %+v", rebuilt.Obligations)
	}
	for _, o := range rebuilt.Obligations {
		if o.Origin == ReasonPolicyFloor && len(o.Targets) > 0 && string(o.Targets[0]) != "" && !containsTarget(o.Targets, "internal/agent/agent.go") {
			t.Fatalf("standard write must not gain a floor obligation: %+v", o)
		}
	}
}

func TestTestsForbiddenDowngradesFloorToAdvisory(t *testing.T) {
	c := New("")
	c.AbsorbReceipt(1, deliveryWriteReceipt(t, "internal/agent/agent.go", PolicyFloorDelivery), "", true, false)
	if !hasObligationKind(c, ObligationFullVerify, EnforcementAdvisory, ReasonPolicyFloor) {
		t.Fatalf("tests-forbidden floor obligation must be advisory: %+v", c.Obligations)
	}
}

func hasUnsatisfiedFloorObligation(c *Contract) bool {
	for _, o := range c.Obligations {
		if o.Origin == ReasonPolicyFloor && !c.obligationSatisfied(o) {
			return true
		}
	}
	return false
}

func containsTarget(targets []evidence.TargetKey, path string) bool {
	for _, t := range targets {
		if len(t) > 0 && t[len(t)-len(path):] == evidence.TargetKey(path) {
			return true
		}
	}
	return false
}

func TestParsePolicyFloorFoldsLegacyVocabulary(t *testing.T) {
	for _, raw := range []string{"", "standard", "balanced", "light", "economy", "eco", "unknown"} {
		if got := ParsePolicyFloor(raw); got != PolicyFloorNone {
			t.Fatalf("ParsePolicyFloor(%q) = %v, want none", raw, got)
		}
	}
	for _, raw := range []string{"delivery", "deliver", "quality"} {
		if got := ParsePolicyFloor(raw); got != PolicyFloorDelivery {
			t.Fatalf("ParsePolicyFloor(%q) = %v, want delivery", raw, got)
		}
	}
}
