package plancontract

import (
	"slices"
	"testing"
)

func orderedTitles(p Plan) []string {
	steps := p.Ordered()
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Title)
	}
	return out
}

func TestOrderedGroupsSubStepsUnderTheirPhase(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "p1", Title: "phase one"},
		{ID: "p2", Title: "phase two"},
		{ID: "b", ParentID: "p2", Title: "two-a"},
		{ID: "a", ParentID: "p1", Title: "one-a"},
	}}.Normalize()

	got := orderedTitles(p)
	want := []string{"phase one", "one-a", "phase two", "two-a"}
	if !slices.Equal(got, want) {
		t.Fatalf("ordered = %v, want %v", got, want)
	}
}

func TestOrderedRespectsSiblingDependencies(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "p", Title: "phase"},
		{ID: "late", ParentID: "p", Title: "late", DependsOn: []string{"early"}},
		{ID: "early", ParentID: "p", Title: "early"},
	}}.Normalize()

	got := orderedTitles(p)
	want := []string{"phase", "early", "late"}
	if !slices.Equal(got, want) {
		t.Fatalf("ordered = %v, want %v", got, want)
	}
}

func TestOrderedIgnoresDependenciesAcrossPhases(t *testing.T) {
	// A sub-step of phase two depending on a sub-step of phase one cannot
	// reorder anything: the phases already order them.
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "p1", Title: "phase one"},
		{ID: "a", ParentID: "p1", Title: "one-a", DependsOn: []string{"b"}},
		{ID: "p2", Title: "phase two"},
		{ID: "b", ParentID: "p2", Title: "two-a"},
	}}.Normalize()

	got := orderedTitles(p)
	want := []string{"phase one", "one-a", "phase two", "two-a"}
	if !slices.Equal(got, want) {
		t.Fatalf("ordered = %v, want %v", got, want)
	}
}

func TestOrderedKeepsEveryStepThroughADependencyCycle(t *testing.T) {
	p := Plan{Objective: "o", Steps: []Step{
		{ID: "a", Title: "a", DependsOn: []string{"b"}},
		{ID: "b", Title: "b", DependsOn: []string{"a"}},
	}}.Normalize()

	got := orderedTitles(p)
	want := []string{"a", "b"}
	if !slices.Equal(got, want) {
		t.Fatalf("ordered = %v, want declared order %v", got, want)
	}
}

func TestOrderedKeepsEveryStepOnUnnormalizedInput(t *testing.T) {
	// Ordered must be total: a caller that skips Normalize gets the same steps,
	// grouped the same way, never a silently shorter list.
	raw := Plan{Objective: "o", Steps: []Step{
		{ID: "p", Title: "phase"},
		{ID: "child", ParentID: "p", Title: "child"},
		{ID: "grandchild", ParentID: "child", Title: "grandchild"},
		{ID: "orphan", ParentID: "ghost", Title: "orphan"},
	}}

	got := orderedTitles(raw)
	if len(got) != len(raw.Steps) {
		t.Fatalf("ordered dropped steps: %v", got)
	}
	if !slices.Equal(got, orderedTitles(raw.Normalize())) {
		t.Fatalf("ordered disagrees with the normalized plan: %v vs %v", got, orderedTitles(raw.Normalize()))
	}
}
