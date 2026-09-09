package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/plancontract"
)

func submitPlan(t *testing.T, ctx context.Context, args string) (string, error) {
	t.Helper()
	return (&SubmitPlanTool{}).Execute(ctx, json.RawMessage(args))
}

const wellFormedPlanArgs = `{
  "objective":"make the cache key model-aware",
  "assumptions":[{"text":"warm caches are disposable","confirm":"rg cacheKey internal/provider"}],
  "steps":[
    {"id":"p1","title":"thread the model ref through","verified_files":["internal/provider/cache.go"],"candidate_files":["internal/boot/boot.go"]},
    {"id":"s1","parent_id":"p1","title":"extend cacheKey",
     "acceptance":[{"text":"two model refs never share an entry"},{"text":"existing hits keep hitting","regression":true}],
     "verification":[{"command":"go test ./internal/provider/","expect":"all green"}]},
    {"id":"p2","title":"record the hit rate"}
  ]
}`

func TestSubmitPlanRecordsAStructuredPlan(t *testing.T) {
	ctx, submission := WithPlanSubmission(context.Background())
	out, err := submitPlan(t, ctx, wellFormedPlanArgs)
	if err != nil {
		t.Fatalf("submit_plan: %v", err)
	}
	if !strings.Contains(out, "revision 1") || !strings.Contains(out, "2 phase(s), 1 sub-step(s)") {
		t.Fatalf("result = %q", out)
	}
	plan, ok := submission.Plan()
	if !ok {
		t.Fatal("submission holds no plan")
	}
	if plan.Objective != "make the cache key model-aware" || len(plan.Steps) != 3 {
		t.Fatalf("plan = %+v", plan)
	}
	step := plan.Steps[1]
	if len(step.VerifiedFiles) != 0 || len(step.Acceptance) != 2 || !step.Acceptance[1].Regression {
		t.Fatalf("sub-step lost its fields: %+v", step)
	}
	if plan.Steps[0].VerifiedFiles[0] != "internal/provider/cache.go" || plan.Steps[0].CandidateFiles[0] != "internal/boot/boot.go" {
		t.Fatalf("verified/candidate surfaces did not survive: %+v", plan.Steps[0])
	}
}

// Identity is host-assigned: a planner that claims one must not get it, which is
// why the fields carry json:"-" rather than a prompt rule.
func TestSubmitPlanIgnoresPlannerAssignedIdentity(t *testing.T) {
	ctx, submission := WithPlanSubmission(context.Background())
	_, err := submitPlan(t, ctx, `{"id":"forged","revision":42,"objective":"o","steps":[{"title":"do it"}]}`)
	if err != nil {
		t.Fatalf("submit_plan: %v", err)
	}
	plan, _ := submission.Plan()
	if plan.ID != "" {
		t.Errorf("planner-supplied plan id survived: %q", plan.ID)
	}
	if plan.Revision != 1 {
		t.Errorf("revision = %d, want the host's count of 1", plan.Revision)
	}
}

func TestSubmitPlanCountsRevisions(t *testing.T) {
	ctx, submission := WithPlanSubmission(context.Background())
	for range 3 {
		if _, err := submitPlan(t, ctx, `{"objective":"o","steps":[{"title":"do it"}]}`); err != nil {
			t.Fatalf("submit_plan: %v", err)
		}
	}
	plan, _ := submission.Plan()
	if plan.Revision != 3 {
		t.Fatalf("revision = %d, want 3", plan.Revision)
	}
}

func TestSubmitPlanReturnsAnActionableValidationError(t *testing.T) {
	ctx, submission := WithPlanSubmission(context.Background())
	_, err := submitPlan(t, ctx, `{"objective":"","steps":[]}`)
	if err == nil {
		t.Fatal("an empty plan must be rejected")
	}
	for _, want := range []string{"no objective", "no steps"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q so the planner can fix it in one round", err, want)
		}
	}
	if _, ok := submission.Plan(); ok {
		t.Fatal("a rejected plan must not be recorded")
	}
}

func TestSubmitPlanRefusesOutsideAPlanningTurn(t *testing.T) {
	_, err := submitPlan(t, context.Background(), wellFormedPlanArgs)
	if err == nil || !strings.Contains(err.Error(), "only available while planning") {
		t.Fatalf("error = %v", err)
	}
	if (&SubmitPlanTool{}).ProviderVisible(context.Background()) {
		t.Fatal("submit_plan must not read as available outside a planning turn")
	}
	ctx, _ := WithPlanSubmission(context.Background())
	if !(&SubmitPlanTool{}).ProviderVisible(ctx) {
		t.Fatal("submit_plan must be available once the host arms the turn")
	}
}

func TestSubmitPlanIsReadOnlyAndInThePlannerRegistry(t *testing.T) {
	if !(&SubmitPlanTool{}).ReadOnly() {
		t.Fatal("submitting a plan touches nothing and must be read-only")
	}
	reg := PlannerToolRegistry(nil)
	if _, ok := reg.Get("submit_plan"); !ok {
		t.Fatalf("planner registry lacks submit_plan: %v", reg.Names())
	}
}

func TestPlannerOutcomeReadsApprovalFromTheFieldWhenStructured(t *testing.T) {
	// requires_approval is the only approval source; rendered prose never gates.
	structured := plannerOutcome{
		text: "The plan is ready. Waiting for approval before I continue.",
		plan: plancontract.Plan{Objective: "o", Steps: []plancontract.Step{{Title: "do it"}}},
	}
	if structured.requestsApproval() {
		t.Fatal("approval prose must not gate a plan whose requires_approval is false")
	}
	structured.plan.RequiresApproval = true
	if !structured.requestsApproval() {
		t.Fatal("requires_approval must gate execution")
	}
}

// The evidence contract is only as strong as its enforcement: a prompt sentence
// can be ignored, a schema field cannot be filled with a claim of another kind.
func TestSubmitPlanSchemaCarriesTheEvidenceContract(t *testing.T) {
	schema := string((&SubmitPlanTool{}).Schema())
	for _, want := range []string{
		"verified_files", "candidate_files", "acceptance", "verification",
		"regression", "assumptions", "requires_approval", "depends_on", "parent_id",
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("submit_plan schema missing %q", want)
		}
	}
	if strings.Contains(schema, "revision") {
		t.Error("submit_plan schema exposes revision; identity is host-assigned")
	}
}

// A planner that resubmits is told what its own edit changed. Left to describe
// it, a model reports intent rather than effect — and a step it dropped by
// accident reads exactly like one it meant to keep.
func TestSubmitPlanTellsARevisionWhatItChanged(t *testing.T) {
	ctx, _ := WithPlanSubmission(context.Background())
	first := `{"objective":"o","steps":[{"id":"s1","title":"change the DB"},{"id":"s2","title":"change the API"}]}`
	if _, err := submitPlan(t, ctx, first); err != nil {
		t.Fatalf("first submission: %v", err)
	}
	second := `{"objective":"o","steps":[{"id":"s1","title":"change the DB"},{"id":"s3","title":"add the migration"},{"id":"s2","title":"change the API"}]}`
	out, err := submitPlan(t, ctx, second)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	for _, want := range []string{"Revision 1 → 2", "**Added**", "s3", "expands the approved scope"} {
		if !strings.Contains(out, want) {
			t.Errorf("revision result missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "**Changed**") {
		t.Errorf("an insertion must not report its neighbours as changed:\n%s", out)
	}
}

func TestSubmitPlanSaysNothingAboutAFirstSubmission(t *testing.T) {
	ctx, _ := WithPlanSubmission(context.Background())
	out, err := submitPlan(t, ctx, `{"objective":"o","steps":[{"title":"do it"}]}`)
	if err != nil {
		t.Fatalf("submit_plan: %v", err)
	}
	if strings.Contains(out, "Revision") && strings.Contains(out, "→") {
		t.Errorf("a first submission replaces nothing and must not render a diff:\n%s", out)
	}
}
