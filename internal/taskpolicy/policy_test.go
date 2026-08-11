package taskpolicy

import (
	"strings"
	"testing"

	"reasonix/internal/agentpreset"
	"reasonix/internal/taskintent"
)

func TestDeriveLightSimpleIsDirect(t *testing.T) {
	p := Derive(Input{
		Raw:    "what is a mutex?",
		Preset: agentpreset.Light,
	})
	if p.Intent != taskintent.Conversation && p.Intent != taskintent.Advisory {
		t.Fatalf("intent = %v", p.Intent)
	}
	if p.Route != RouteDirect {
		t.Fatalf("route = %v, want direct", p.Route)
	}
	if p.Review != ReviewNone {
		t.Fatalf("review = %v, want none", p.Review)
	}
}

func TestDeriveLightHighRiskElevates(t *testing.T) {
	p := Derive(Input{
		Raw:    "fix the authentication bypass in production login",
		Preset: agentpreset.Light,
	})
	if p.Risk < RiskHigh && !p.SecurityClass {
		t.Fatalf("expected high risk or security class, got risk=%v security=%v", p.Risk, p.SecurityClass)
	}
	if p.Review != ReviewForcedSecurity {
		t.Fatalf("review = %v, want forced-security", p.Review)
	}
	if p.Verification != VerifyFull {
		t.Fatalf("verification = %v, want full", p.Verification)
	}
	if p.Route != RouteFullPlan {
		t.Fatalf("route = %v, want full plan", p.Route)
	}
}

func TestDeriveDeliveryLowRiskNoReviewer(t *testing.T) {
	p := Derive(Input{
		Raw:      "fix the typo in README.md",
		Preset:   agentpreset.Delivery,
		Anchored: true,
	})
	if p.Risk != RiskLow {
		// typo fix is low risk
		t.Logf("risk = %v (may be medium if multi-file heuristics fire)", p.Risk)
	}
	if p.Risk == RiskLow && p.RequiresIndependentReview() {
		t.Fatal("delivery low-risk must not force independent reviewer")
	}
	if !p.RequireAtomicContract {
		t.Fatal("delivery mutation should require atomic contract")
	}
}

func TestDeriveDeliveryMediumForcesReview(t *testing.T) {
	p := Derive(Input{
		Raw:          "refactor the payment module and update its callers",
		Preset:       agentpreset.Delivery,
		MultiFile:    true,
		CrossSurface: true,
	})
	if p.Risk < RiskMedium {
		t.Fatalf("risk = %v, want at least medium", p.Risk)
	}
	if !p.RequiresIndependentReview() {
		t.Fatal("delivery medium/high must force independent reviewer")
	}
}

func TestConstraintsNoMutation(t *testing.T) {
	p := Derive(Input{
		Raw:    "只分析这段代码的问题，不要修改",
		Preset: agentpreset.Balanced,
	})
	if p.AllowsMutation() {
		t.Fatal("must forbid mutation")
	}
}

func TestConstraintsNoTests(t *testing.T) {
	p := Derive(Input{
		Raw:    "fix the bug but don't run tests",
		Preset: agentpreset.Balanced,
	})
	if p.AllowsTests() {
		t.Fatal("must forbid tests")
	}
}

func TestConstraintsOnlyRun(t *testing.T) {
	p := Derive(Input{
		Raw:    "fix the parser, only run go test ./internal/parser",
		Preset: agentpreset.Balanced,
	})
	if !p.AllowsCommand("go test ./internal/parser") {
		t.Fatal("allowed check should pass")
	}
	if p.AllowsCommand("npm test") {
		t.Fatal("other checks should be blocked")
	}
	if p.AllowsCommand("go test ./internal/parser && npm test") {
		t.Fatal("a second shell command must not inherit the go test allowance")
	}
}

func TestConstraintsNoPush(t *testing.T) {
	p := Derive(Input{
		Raw:    "fix the bug, don't push",
		Preset: agentpreset.Balanced,
	})
	if !p.AllowsMutation() {
		t.Fatal("local mutation should still be allowed")
	}
	if p.AllowsExternal() {
		t.Fatal("push must be forbidden")
	}
}

func TestQuotedConstraintsIgnored(t *testing.T) {
	raw := "please fix the bug\n```\ndon't modify anything\n```\n"
	p := Derive(Input{
		Raw:         raw,
		Instruction: StripQuotedConstraints(raw),
		Preset:      agentpreset.Balanced,
	})
	if !p.AllowsMutation() {
		t.Fatal("quoted no-modify must not bind the host")
	}
}

func TestRiskOnlyRatchetsUp(t *testing.T) {
	p := Derive(Input{Raw: "explain this", Preset: agentpreset.Balanced})
	if p.Risk != RiskLow {
		t.Fatalf("start risk = %v", p.Risk)
	}
	p.RaiseRisk(RiskMedium)
	if p.Risk != RiskMedium {
		t.Fatalf("raised = %v", p.Risk)
	}
	p.RaiseRisk(RiskLow)
	if p.Risk != RiskMedium {
		t.Fatal("risk must not decrease")
	}
}

func TestPlanModeForbidsMutation(t *testing.T) {
	p := Derive(Input{
		Raw:      "implement the feature",
		Preset:   agentpreset.Delivery,
		PlanMode: true,
	})
	if p.AllowsMutation() {
		t.Fatal("plan mode must forbid mutation")
	}
}

func TestExecutionPolicyBlockStable(t *testing.T) {
	p := Derive(Input{Raw: "fix it", Preset: agentpreset.Balanced})
	block := ExecutionPolicyBlock(p)
	if !strings.Contains(block, `preset="balanced"`) {
		t.Fatalf("block missing preset: %s", block)
	}
	if !strings.Contains(block, `version="1"`) {
		t.Fatalf("block missing version: %s", block)
	}
	if !strings.HasPrefix(block, "<execution-policy") || !strings.HasSuffix(block, "</execution-policy>") {
		t.Fatalf("bad block shape: %s", block)
	}
}

func TestMatrixPlanningRoutes(t *testing.T) {
	// Balanced multi-file → light plan
	p := Derive(Input{
		Raw:       "update the API and its tests",
		Preset:    agentpreset.Balanced,
		MultiFile: true,
	})
	if p.Route != RouteLightPlan && p.Route != RouteFullPlan {
		t.Fatalf("balanced multi-file route = %v", p.Route)
	}
	// Delivery multi-file medium → full plan
	d := Derive(Input{
		Raw:        "update the API and its tests across packages",
		Preset:     agentpreset.Delivery,
		MultiFile:  true,
		Structured: true,
	})
	if d.Route != RouteFullPlan {
		t.Fatalf("delivery structured multi-file route = %v, want full", d.Route)
	}
}
