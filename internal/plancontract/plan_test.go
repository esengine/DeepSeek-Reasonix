package plancontract

import (
	"strings"
	"testing"
)

func TestNormalizeAssignsMissingIDsWithoutColliding(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{Title: "first"},
		{ID: "s1", Title: "second"},
		{Title: "third"},
	}}.Normalize()

	got := []string{p.Steps[0].ID, p.Steps[1].ID, p.Steps[2].ID}
	want := []string{"plan_step_01", "s1", "plan_step_02"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step ids = %v, want %v", got, want)
		}
	}
}

func TestNormalizeReassignsDuplicateStepIDs(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "a", Title: "first"},
		{ID: "a", Title: "second"},
	}}.Normalize()

	if p.Steps[0].ID != "a" {
		t.Fatalf("first step id = %q, want the submitted %q", p.Steps[0].ID, "a")
	}
	if p.Steps[1].ID == "a" {
		t.Fatal("duplicate step id survived normalization")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("normalized plan must validate: %v", err)
	}
}

func TestNormalizeAssignsCriterionIDsAcrossSteps(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "a", Title: "first", Acceptance: []Criterion{{Text: "one"}}},
		{ID: "b", Title: "second", Acceptance: []Criterion{{Text: "two"}, {Text: "three"}}},
	}}.Normalize()

	seen := map[string]bool{}
	for _, step := range p.Steps {
		for _, c := range step.Acceptance {
			if c.ID == "" {
				t.Fatalf("criterion %q has no id", c.Text)
			}
			if seen[c.ID] {
				t.Fatalf("criterion id %q reused across steps", c.ID)
			}
			seen[c.ID] = true
		}
	}
}

func TestNormalizeDropsEmptyEntriesAndDeduplicates(t *testing.T) {
	p := Plan{
		Objective:   "  ship it  ",
		NonGoals:    []string{" rewrite the world ", "", "rewrite the world"},
		Assumptions: []Assumption{{Text: "  "}, {Text: "cache is warm", Confirm: " check hit rate "}},
		Steps: []Step{
			{Title: "   "},
			{Title: " do it ", VerifiedFiles: []string{"a.go", "a.go", " b.go "}, Risks: []string{""},
				Acceptance:   []Criterion{{Text: "  "}},
				Verification: []Verification{{Command: " ", Expect: " "}}},
		},
	}.Normalize()

	if p.Objective != "ship it" {
		t.Fatalf("objective = %q", p.Objective)
	}
	if len(p.NonGoals) != 1 || p.NonGoals[0] != "rewrite the world" {
		t.Fatalf("non-goals = %v", p.NonGoals)
	}
	if len(p.Assumptions) != 1 || p.Assumptions[0].Confirm != "check hit rate" {
		t.Fatalf("assumptions = %+v", p.Assumptions)
	}
	if len(p.Steps) != 1 || p.Steps[0].Title != "do it" {
		t.Fatalf("steps = %+v", p.Steps)
	}
	step := p.Steps[0]
	if len(step.VerifiedFiles) != 2 || step.VerifiedFiles[1] != "b.go" {
		t.Fatalf("verified files = %v", step.VerifiedFiles)
	}
	if step.Risks != nil || step.Acceptance != nil || step.Verification != nil {
		t.Fatalf("empty entries survived: %+v", step)
	}
}

func TestNormalizeRepairsParentReferences(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "phase", Title: "phase"},
		{ID: "child", ParentID: "phase", Title: "child"},
		{ID: "grandchild", ParentID: "child", Title: "grandchild"},
		{ID: "orphan", ParentID: "nobody", Title: "orphan"},
		{ID: "self", ParentID: "self", Title: "self"},
	}}.Normalize()

	want := map[string]string{
		"phase": "", "child": "phase", "grandchild": "phase", "orphan": "", "self": "",
	}
	for _, step := range p.Steps {
		if got := step.ParentID; got != want[step.ID] {
			t.Errorf("step %q parent = %q, want %q", step.ID, got, want[step.ID])
		}
	}
}

func TestNormalizeDropsUnresolvableDependencies(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "a", Title: "a", DependsOn: []string{"a", "ghost", "b", "b"}},
		{ID: "b", Title: "b"},
	}}.Normalize()

	got := p.Steps[0].DependsOn
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("depends-on = %v, want [b]", got)
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	once := Plan{Objective: "o", Steps: []Step{
		{Title: "phase"},
		{ParentID: "s1", Title: "child", DependsOn: []string{"s1"}},
	}}.Normalize()
	twice := once.Normalize()

	if len(once.Steps) != len(twice.Steps) {
		t.Fatalf("step count changed: %d then %d", len(once.Steps), len(twice.Steps))
	}
	for i := range once.Steps {
		if once.Steps[i].ID != twice.Steps[i].ID || once.Steps[i].ParentID != twice.Steps[i].ParentID {
			t.Fatalf("step %d changed on re-normalize: %+v then %+v", i, once.Steps[i], twice.Steps[i])
		}
	}
}

func TestValidateRejectsMissingObjectiveAndSteps(t *testing.T) {
	err := Plan{}.Validate()
	if err == nil {
		t.Fatal("empty plan must not validate")
	}
	for _, want := range []string{"no objective", "no steps"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestValidateRejectsOversizedPlan(t *testing.T) {
	steps := make([]Step, MaxSteps+1)
	for i := range steps {
		steps[i] = Step{Title: "step"}
	}
	err := Plan{Objective: "o", Steps: steps}.Normalize().Validate()
	if err == nil || !strings.Contains(err.Error(), "the limit is") {
		t.Fatalf("oversized plan error = %v", err)
	}
}

func TestValidateRejectsRawDuplicateIDs(t *testing.T) {
	err := Plan{Objective: "o", Steps: []Step{
		{ID: "a", Title: "one"},
		{ID: "a", Title: "two"},
	}}.Validate()
	if err == nil || !strings.Contains(err.Error(), "used more than once") {
		t.Fatalf("duplicate id error = %v", err)
	}
}

func TestValidateReportsDependencyCycle(t *testing.T) {
	err := Plan{Objective: "o", Steps: []Step{
		{ID: "a", Title: "a", DependsOn: []string{"b"}},
		{ID: "b", Title: "b", DependsOn: []string{"a"}},
	}}.Normalize().Validate()
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestValidateAcceptsWellFormedPlan(t *testing.T) {
	p := Plan{
		Objective: "make the cache key model-aware",
		Steps: []Step{
			{ID: "p1", Title: "thread the model ref through"},
			{ID: "s1", ParentID: "p1", Title: "extend cacheKey"},
			{ID: "s2", ParentID: "p1", Title: "update callers", DependsOn: []string{"s1"}},
		},
	}.Normalize()
	if err := p.Validate(); err != nil {
		t.Fatalf("well-formed plan must validate: %v", err)
	}
}
