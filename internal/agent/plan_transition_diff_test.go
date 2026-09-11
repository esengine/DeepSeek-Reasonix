package agent

import (
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

func todoList(items ...[2]string) []evidence.TodoItem {
	out := make([]evidence.TodoItem, 0, len(items))
	for _, it := range items {
		out = append(out, evidence.TodoItem{StepID: it[0], Content: it[1], Status: "pending"})
	}
	return out
}

// The reviewer used to receive two renderings and have to diff them itself,
// which a clip can cut in half. Pairing on step ids says what moved.
func TestPlanTransitionDiffNamesTheInsertion(t *testing.T) {
	before := todoList([2]string{"plan_step_01", "change the DB"}, [2]string{"plan_step_02", "change the API"})
	after := todoList(
		[2]string{"plan_step_01", "change the DB"},
		[2]string{"plan_step_03", "add the migration"},
		[2]string{"plan_step_02", "change the API"},
	)
	got := planTransitionDiff(before, after)
	for _, want := range []string{"**Added**", "plan_step_03", "**Preserved**"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "**Changed**") {
		t.Errorf("an insertion must not report its neighbours as changed:\n%s", got)
	}
}

func TestPlanTransitionDiffReportsARetitleAsChanged(t *testing.T) {
	before := todoList([2]string{"plan_step_01", "fix authentication"})
	after := todoList([2]string{"plan_step_01", "fix authentication refresh flow"})
	got := planTransitionDiff(before, after)
	if !strings.Contains(got, "**Changed**") || !strings.Contains(got, "title") {
		t.Fatalf("a retitle under a kept id is a change, not a replacement:\n%s", got)
	}
}

// A list that never carried ids cannot be paired: every step would read as
// replaced, which is worse than the plain renderings the reviewer already has.
func TestPlanTransitionDiffStandsDownWithoutIdentity(t *testing.T) {
	before := []evidence.TodoItem{{Content: "change the DB", Status: "pending"}}
	after := []evidence.TodoItem{{Content: "change the DB", Status: "pending"}, {Content: "and more", Status: "pending"}}
	if got := planTransitionDiff(before, after); got != "" {
		t.Fatalf("diff = %q, want none without step ids", got)
	}
}

func TestPlanTransitionDiffSaysNothingWhenNothingMoved(t *testing.T) {
	same := todoList([2]string{"plan_step_01", "change the DB"})
	if got := planTransitionDiff(same, same); got != "" {
		t.Fatalf("diff = %q, want none", got)
	}
}
