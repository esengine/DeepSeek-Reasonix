package plancontract

import (
	"slices"
	"strings"
	"testing"
)

func diffIDs(steps []Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

func basePlan() Plan {
	return Plan{
		Objective: "make the cache key model-aware",
		Revision:  1,
		Steps: []Step{
			{ID: "s1", Title: "change the DB"},
			{ID: "s2", Title: "change the API", CandidateFiles: []string{"api.go"}},
			{ID: "s3", Title: "write tests"},
		},
	}.Normalize()
}

// The whole reason identity is host-assigned and never regenerated: a diff that
// paired by position would call every step below an insertion "changed".
func TestCompareParesByIdentityNotPosition(t *testing.T) {
	after := basePlan()
	after.Revision = 2
	after.Steps = []Step{
		after.Steps[0],
		{ID: "s4", Title: "add the migration"},
		after.Steps[1],
		after.Steps[2],
	}

	d := Compare(basePlan(), after.Normalize())
	if !slices.Equal(diffIDs(d.Added), []string{"s4"}) {
		t.Errorf("added = %v, want only the inserted step", diffIDs(d.Added))
	}
	if len(d.Changed) != 0 {
		t.Errorf("changed = %+v; an insertion must not mark its neighbours changed", d.Changed)
	}
	if !slices.Equal(diffIDs(d.Preserved), []string{"s1", "s2", "s3"}) {
		t.Errorf("preserved = %v, want every untouched step", diffIDs(d.Preserved))
	}
}

// "The title was reworded" and "the acceptance criteria were rewritten" carry
// different risk, so the diff names which part moved.
func TestCompareNamesWhichFieldsMoved(t *testing.T) {
	after := basePlan()
	after.Revision = 2
	after.Steps[1].Title = "change the API and its schema"
	after.Steps[1].CandidateFiles = []string{"api.go", "schema.go"}

	d := Compare(basePlan(), after.Normalize())
	if len(d.Changed) != 1 {
		t.Fatalf("changed = %+v, want one step", d.Changed)
	}
	if got := d.Changed[0].Fields; !slices.Equal(got, []string{"title", "candidate_files"}) {
		t.Fatalf("fields = %v, want the two that moved", got)
	}
}

func TestCompareReportsRemoval(t *testing.T) {
	after := basePlan()
	after.Revision = 2
	after.Steps = after.Steps[:2]

	d := Compare(basePlan(), after.Normalize())
	if !slices.Equal(diffIDs(d.Removed), []string{"s3"}) {
		t.Fatalf("removed = %v", diffIDs(d.Removed))
	}
}

// Re-asking for a narrowing trains the user to approve without reading, which
// costs more than the gate saves.
func TestNeedsApprovalOnlyOnExpansion(t *testing.T) {
	base := basePlan()
	widen := func(mutate func(*Plan)) Diff {
		after := basePlan()
		after.Revision = 2
		mutate(&after)
		return Compare(base, after.Normalize())
	}

	expansions := map[string]func(*Plan){
		"a new step":      func(p *Plan) { p.Steps = append(p.Steps, Step{ID: "s4", Title: "extra"}) },
		"a new objective": func(p *Plan) { p.Objective = "something else entirely" },
		"a new risk":      func(p *Plan) { p.Steps[0].Risks = []string{"data loss"} },
		"a new criterion": func(p *Plan) { p.Steps[0].Acceptance = []Criterion{{Text: "must hold"}} },
		"a wider surface": func(p *Plan) { p.Steps[1].CandidateFiles = []string{"api.go", "schema.go"} },
	}
	for name, mutate := range expansions {
		if !widen(mutate).NeedsApproval() {
			t.Errorf("%s must ask again", name)
		}
	}

	narrowings := map[string]func(*Plan){
		"a dropped step":   func(p *Plan) { p.Steps = p.Steps[:2] },
		"a reworded title": func(p *Plan) { p.Steps[0].Title = "change the database" },
		"a reorder":        func(p *Plan) { p.Steps[0], p.Steps[2] = p.Steps[2], p.Steps[0] },
	}
	for name, mutate := range narrowings {
		if widen(mutate).NeedsApproval() {
			t.Errorf("%s must not ask again", name)
		}
	}
}

func TestRenderDiffOmitsEmptySectionsAndSaysNothingWhenNothingMoved(t *testing.T) {
	if got := RenderDiff(Compare(basePlan(), basePlan())); got != "" {
		t.Fatalf("an unchanged plan rendered %q", got)
	}

	after := basePlan()
	after.Revision = 2
	after.Steps = append(after.Steps, Step{ID: "s4", Title: "add the migration"})
	out := RenderDiff(Compare(basePlan(), after.Normalize()))
	for _, want := range []string{"Revision 1 → 2", "**Added**", "s4  add the migration", "**Preserved**"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff missing %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"**Removed**", "**Changed**", "**Objective**"} {
		if strings.Contains(out, absent) {
			t.Errorf("diff should omit %q:\n%s", absent, out)
		}
	}
}
