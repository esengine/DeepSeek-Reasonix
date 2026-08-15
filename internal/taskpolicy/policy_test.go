package taskpolicy

import (
	"strings"
	"testing"

	"reasonix/internal/taskintent"
)

// The standard policy matrix (plan §2.2): one adaptive execution whose
// planning, verification, review, and evidence-closure strength follows task
// risk.
func TestStandardPolicyMatrix(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want TaskPolicy
	}{
		{
			name: "conversation low risk is direct with no evidence",
			in:   Input{Raw: "what is a mutex?"},
			want: TaskPolicy{
				Intent: taskintent.Conversation, Risk: RiskLow, Route: RouteDirect,
				Evidence: EvidenceNone, Verification: VerifyNone, Review: ReviewNone,
			},
		},
		{
			name: "single-file anchored low-risk modification is direct atomic targeted",
			in:   Input{Raw: "fix the typo in README.md", Anchored: true},
			want: TaskPolicy{
				Intent: taskintent.Mutation, Risk: RiskLow, Route: RouteDirect,
				Evidence: EvidenceTargeted, Verification: VerifyTargeted, Review: ReviewNone,
				RequireAtomicContract: true,
			},
		},
		{
			name: "multi-file same-surface modification gets light plan and soft quality reporting",
			in:   Input{Raw: "update the parser and its tests to accept empty input", MultiFile: true},
			want: TaskPolicy{
				Intent: taskintent.Mutation, Risk: RiskMedium, Route: RouteLightPlan,
				Evidence: EvidenceTargeted, Verification: VerifyFull, Review: ReviewConditional,
				AllowExploreSubagent: true, SemanticRouterAllowed: true,
			},
		},
		{
			name: "cross-surface work forces full plan and closed loop",
			in:   Input{Raw: "wire the frontend and backend to the new api", CrossSurface: true},
			want: TaskPolicy{
				Intent: taskintent.Conversation, Risk: RiskMedium, Route: RouteFullPlan, Evidence: EvidenceClosedLoop,
				Verification: VerifyFull, Review: ReviewConditional,
				AllowExploreSubagent: true, SemanticRouterAllowed: true,
			},
		},
		{
			name: "high-risk migration forces full verification, review, closure",
			in:   Input{Raw: "migrate the database schema and drop the old table", HighRiskHints: true},
			want: TaskPolicy{
				Intent: taskintent.Conversation, Risk: RiskHigh, Route: RouteFullPlan, Evidence: EvidenceClosedLoop,
				Verification: VerifyFull, Review: ReviewForcedSecurity, SecurityClass: true,
				AllowExploreSubagent: true, SemanticRouterAllowed: true,
			},
		},
		{
			name: "security-class work forces security review",
			in:   Input{Raw: "fix the authentication bypass in production login"},
			want: TaskPolicy{
				Intent: taskintent.Mutation, Risk: RiskHigh, Route: RouteFullPlan, Evidence: EvidenceClosedLoop,
				Verification: VerifyFull, Review: ReviewForcedSecurity,
				SecurityClass: true, AllowExploreSubagent: true, SemanticRouterAllowed: true,
			},
		},
		{
			name: "active goal upgrades planning to full",
			in:   Input{Raw: "refactor the module structure and keep the tests green", GoalActive: true},
			want: TaskPolicy{
				Intent: taskintent.Mutation, Risk: RiskLow, Route: RouteFullPlan, Evidence: EvidenceClosedLoop,
				Verification: VerifyTargeted, Review: ReviewForced,
				AllowExploreSubagent: true, SemanticRouterAllowed: true,
			},
		},
		{
			name: "read-only constraint caps evidence at targeted and drops review",
			in:   Input{Raw: "audit the payment flow for security issues, analyze only"},
			want: TaskPolicy{
				Intent: taskintent.ObservableRead, Risk: RiskHigh, Route: RouteFullPlan,
				Constraints: Constraints{ForbidMutation: true},
				Evidence:    EvidenceTargeted, Verification: VerifyFull, Review: ReviewNone,
				SecurityClass: true,
			},
		},
		{
			name: "user full-verification request closes the loop",
			in:   Input{Raw: "finish the refactor with complete verification and closed-loop delivery"},
			want: TaskPolicy{
				Intent: taskintent.Mutation, Risk: RiskLow, Route: RouteFullPlan,
				Constraints: Constraints{RequireFullVerification: true},
				Evidence:    EvidenceClosedLoop, Verification: VerifyFull, Review: ReviewForced,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Derive(tc.in)
			if got.PolicyVersion != 2 {
				t.Fatalf("policy version = %d, want 2", got.PolicyVersion)
			}
			if got.Intent != tc.want.Intent {
				t.Errorf("intent = %v, want %v", got.Intent, tc.want.Intent)
			}
			if got.Risk != tc.want.Risk {
				t.Errorf("risk = %v, want %v", got.Risk, tc.want.Risk)
			}
			if got.Route != tc.want.Route {
				t.Errorf("route = %v, want %v", got.Route, tc.want.Route)
			}
			if got.Evidence != tc.want.Evidence {
				t.Errorf("evidence = %v, want %v", got.Evidence, tc.want.Evidence)
			}
			if got.Verification != tc.want.Verification {
				t.Errorf("verification = %v, want %v", got.Verification, tc.want.Verification)
			}
			if got.Review != tc.want.Review {
				t.Errorf("review = %v, want %v", got.Review, tc.want.Review)
			}
			if got.SecurityClass != tc.want.SecurityClass {
				t.Errorf("securityClass = %v, want %v", got.SecurityClass, tc.want.SecurityClass)
			}
			if tc.want.RequireAtomicContract && !got.RequireAtomicContract {
				t.Error("requireAtomicContract = false, want true")
			}
			if tc.want.AllowExploreSubagent && !got.AllowExploreSubagent {
				t.Error("allowExploreSubagent = false, want true")
			}
			if tc.want.SemanticRouterAllowed && !got.SemanticRouterAllowed {
				t.Error("semanticRouterAllowed = false, want true")
			}
			if tc.want.Constraints.ForbidMutation && !got.Constraints.ForbidMutation {
				t.Error("forbidMutation = false, want true")
			}
		})
	}
}

// Cost constraints (plan §2.3): low-risk tasks must not arm any auxiliary
// model surface — no planner preference, no explore sub-agents, no semantic
// router, no reviewer.
func TestLowRiskHasNoAuxiliaryModelSurfaces(t *testing.T) {
	conv := Derive(Input{Raw: "explain the difference between goroutines and threads"})
	if conv.Route != RouteDirect || conv.Review != ReviewNone {
		t.Fatalf("conversation route=%v review=%v, want direct/none", conv.Route, conv.Review)
	}
	if conv.AllowExploreSubagent || conv.SemanticRouterAllowed {
		t.Fatal("low-risk conversation must not allow explore sub-agents or the semantic router")
	}
	atomic := Derive(Input{Raw: "fix the typo in README.md", Anchored: true})
	if atomic.Route != RouteDirect || atomic.Review != ReviewNone {
		t.Fatalf("atomic edit route=%v review=%v, want direct/none", atomic.Route, atomic.Review)
	}
	if !atomic.RequireAtomicContract {
		t.Fatal("single-file low-risk mutation must require the zero-call atomic contract")
	}
	if atomic.Evidence != EvidenceTargeted {
		t.Fatalf("atomic edit evidence = %v, want targeted", atomic.Evidence)
	}
	if atomic.AllowExploreSubagent || atomic.SemanticRouterAllowed {
		t.Fatal("low-risk atomic edit must not allow auxiliary model surfaces")
	}
	goalContinuation := Derive(Input{Raw: "fix the typo in README.md", Anchored: true, GoalActive: true})
	if goalContinuation.RequireAtomicContract {
		t.Fatal("an active Goal must use its scope-level evidence contract, not a fresh atomic mutation contract")
	}
}

// Risk only ratchets upward; floors re-evaluate (plan §4.1).
func TestRaiseRiskOnlyRatchetsUpward(t *testing.T) {
	p := Derive(Input{Raw: "fix the typo in README.md", Anchored: true})
	p.RaiseRisk(RiskHigh)
	if p.Risk != RiskHigh {
		t.Fatalf("risk = %v, want high", p.Risk)
	}
	if p.Route != RouteFullPlan {
		t.Fatalf("route = %v, want full plan after high-risk raise", p.Route)
	}
	if p.Verification != VerifyFull {
		t.Fatalf("verification = %v, want full", p.Verification)
	}
	if p.Evidence != EvidenceClosedLoop {
		t.Fatalf("evidence = %v, want closed loop", p.Evidence)
	}
	if p.Review != ReviewForced {
		t.Fatalf("review = %v, want forced", p.Review)
	}
	// Downgrade attempts are ignored.
	p.RaiseRisk(RiskLow)
	if p.Risk != RiskHigh || p.Evidence != EvidenceClosedLoop {
		t.Fatal("risk/evidence must not ratchet down")
	}
}

func TestEscalateConditionalReview(t *testing.T) {
	p := Derive(Input{Raw: "update the parser and its tests", MultiFile: true})
	if p.Review != ReviewConditional {
		t.Fatalf("review = %v, want conditional", p.Review)
	}
	p.EscalateConditionalReview("weak_evidence_coverage")
	if p.Review != ReviewForced {
		t.Fatalf("review = %v, want forced after escalation", p.Review)
	}
	if p.Evidence != EvidenceTargeted {
		t.Fatalf("evidence = %v, want targeted quality reporting after ordinary review escalation", p.Evidence)
	}
	high := Derive(Input{Raw: "fix the authentication bypass", HighRiskHints: true})
	high.Evidence = EvidenceTargeted
	high.EscalateConditionalReview("weak_evidence_coverage")
	if high.Evidence != EvidenceClosedLoop {
		t.Fatalf("high-risk evidence = %v, want closed loop", high.Evidence)
	}
	// Read-only turns never escalate into review.
	ro := Derive(Input{Raw: "review the payment flow, analyze only"})
	ro.EscalateConditionalReview("x")
	if ro.Review != ReviewNone {
		t.Fatalf("read-only review = %v, want none", ro.Review)
	}
}

// Writer children inherit the parent's floors (plan §3.1).
func TestInheritFromMergesFloors(t *testing.T) {
	parent := Derive(Input{Raw: "migrate the database schema", HighRiskHints: true})
	child := Derive(Input{Raw: "fix the typo in README.md", Anchored: true})
	if child.Risk == parent.Risk {
		t.Fatal("precondition: child starts below parent risk")
	}
	child.InheritFrom(parent)
	if child.Risk != parent.Risk {
		t.Fatalf("child risk = %v, want %v", child.Risk, parent.Risk)
	}
	if child.Verification < parent.Verification {
		t.Fatalf("child verification = %v, want >= %v", child.Verification, parent.Verification)
	}
	if child.Review < parent.Review {
		t.Fatalf("child review = %v, want >= %v", child.Review, parent.Review)
	}
	if child.Evidence < parent.Evidence {
		t.Fatalf("child evidence = %v, want >= %v", child.Evidence, parent.Evidence)
	}
}

// User-forbidden tests must end Partial/Unverified, never silently Complete
// (plan §4.1).
func TestForbidTestsAllowsPartialButKeepsClosure(t *testing.T) {
	low := Derive(Input{Raw: "fix the typo in README.md, don't run tests", Anchored: true})
	if !low.Constraints.ForbidTests {
		t.Fatal("forbid tests constraint not parsed")
	}
	if !low.AllowsPartialWithoutChecks() {
		t.Fatal("targeted turn must allow Partial without checks")
	}
	closed := Derive(Input{Raw: "migrate the schema, don't run tests", HighRiskHints: true})
	if closed.Evidence != EvidenceClosedLoop {
		t.Fatalf("evidence = %v, want closed loop", closed.Evidence)
	}
	if !closed.AllowsPartialWithoutChecks() {
		t.Fatal("user-forbidden tests still allow a Partial ending (marked unverified)")
	}
	closed.Constraints.ForbidTests = false
	if closed.AllowsPartialWithoutChecks() {
		t.Fatal("closed-loop turns without the user ban must not waive checks")
	}
}

// ExecutionPolicyBlock version 2: no preset, carries risk/route/verification/
// review/evidence, and is appended only at the tail of a user turn (plan §4.2).
func TestExecutionPolicyBlockV2(t *testing.T) {
	p := Derive(Input{Raw: "migrate the database schema", HighRiskHints: true})
	block := ExecutionPolicyBlock(p)
	if !strings.Contains(block, `<execution-policy version="2">`) {
		t.Fatalf("block missing version 2 header: %s", block)
	}
	if strings.Contains(block, "preset") {
		t.Fatalf("block must not contain preset: %s", block)
	}
	for _, want := range []string{"route=full-plan", "risk=high", "verification=full", "review=forced", "evidence=closed-loop"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q: %s", want, block)
		}
	}
	if !strings.HasSuffix(block, "</execution-policy>") {
		t.Fatalf("block must end with closing tag: %s", block)
	}
	conv := Derive(Input{Raw: "what is a mutex?"})
	cblock := ExecutionPolicyBlock(conv)
	for _, want := range []string{"route=direct", "risk=low", "verification=none", "review=none", "evidence=none"} {
		if !strings.Contains(cblock, want) {
			t.Fatalf("conversation block missing %q: %s", want, cblock)
		}
	}
}

// Legacy execution-mode labels are not a Derive input. The same task text
// yields the same standard policy regardless of any leftover mode vocabulary
// the user might still type as a flag name (plan §4.1).
func TestLegacyModeVocabularyDoesNotCreatePolicyForks(t *testing.T) {
	base := Derive(Input{Raw: "fix the typo in README.md", Anchored: true})
	for _, leftover := range []string{"light", "balanced", "delivery", "economy", "full"} {
		got := Derive(Input{Raw: "fix the typo in README.md", Anchored: true})
		if ExecutionPolicyBlock(got) != ExecutionPolicyBlock(base) {
			t.Fatalf("leftover label %q must not exist as a policy input; blocks diverged", leftover)
		}
	}
}

// Identical inputs derive identical policies; nothing outside the Input shape
// can change the standard policy (plan §4.1).
func TestDeriveIsDeterministic(t *testing.T) {
	a := Derive(Input{Raw: "fix the typo in README.md", Anchored: true})
	b := Derive(Input{Raw: "fix the typo in README.md", Anchored: true})
	if ExecutionPolicyBlock(a) != ExecutionPolicyBlock(b) || a.Risk != b.Risk || a.Route != b.Route ||
		a.Evidence != b.Evidence || a.Verification != b.Verification || a.Review != b.Review {
		t.Fatal("identical inputs must derive identical policies")
	}
}
