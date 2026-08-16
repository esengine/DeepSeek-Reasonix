package control

import "testing"

func TestFactDrivenPlannerIsExplicitOnly(t *testing.T) {
	if TaskWarrantsPlanner("解释 OAuth token") || TaskWarrantsPlanner("复杂重构认证迁移") {
		t.Fatal("ordinary wording must stay executor-only")
	}
	if !TaskWarrantsPlanner("先规划再执行认证迁移") {
		t.Fatal("explicit plan-then-execute must call planner")
	}
}

func TestFactDrivenConstraintsIgnoreComplexityWords(t *testing.T) {
	// ParseConstraints lives in runtimepolicy; this covers the controller path
	// that must not auto-plan on complexity words.
	if TaskWarrantsPlanner("复杂重构认证迁移 OAuth token database") {
		t.Fatal("complexity words must not invoke the planner")
	}
}
