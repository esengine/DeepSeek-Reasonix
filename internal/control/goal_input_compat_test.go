package control

import (
	"strings"
	"testing"
)

func TestGoalAutoResearchCanBeForcedOrDisabled(t *testing.T) {
	c := New(Options{})
	c.SetGoalWithResearchMode("fix the typo and add a test", GoalResearchOn)
	if got := c.Compose("start"); strings.Contains(strings.ToLower(got), "autoresearch") || c.goals.budgetClass != budgetClassResearch {
		t.Fatalf("forced research Goal should select the research class: %q %q", got, c.goals.budgetClass)
	}
	if c.GoalRuntime().TurnsLimit != 0 {
		t.Fatalf("forced research compatibility flag selected a turn quota: %+v", c.GoalRuntime())
	}

	c.SetGoalWithResearchMode("持续排查这个线上卡顿直到根因明确", GoalResearchOff)
	if got := c.Compose("start"); strings.Contains(strings.ToLower(got), "autoresearch") || c.goals.budgetClass == budgetClassResearch {
		t.Fatalf("simple override should not select the research class: %q %q", got, c.goals.budgetClass)
	}
	if c.GoalRuntime().TurnsLimit != 0 {
		t.Fatalf("simple compatibility flag selected a turn quota: %+v", c.GoalRuntime())
	}
}

func TestGoalCommandPreservesResearchModeFlags(t *testing.T) {
	c := New(Options{})
	if !c.applyGoalCommand("/goal --research fix the typo", "") {
		t.Fatal("goal command was not parsed")
	}
	if got := c.Compose("start"); strings.Contains(strings.ToLower(got), "autoresearch") || c.goals.budgetClass != budgetClassResearch {
		t.Fatalf("/goal --research should select the research class: %q %q", got, c.goals.budgetClass)
	}
	if c.GoalRuntime().TurnsLimit != 0 {
		t.Fatalf("/goal --research selected a turn quota: %+v", c.GoalRuntime())
	}

	c = New(Options{})
	if !c.applyGoalCommand("/goal --simple 持续排查这个线上卡顿直到根因明确", "") {
		t.Fatal("goal command was not parsed")
	}
	if got := c.Compose("start"); strings.Contains(strings.ToLower(got), "autoresearch") || c.GoalRuntime().TurnsLimit != 0 {
		t.Fatalf("/goal --simple must not select a turn quota: %q %+v", got, c.GoalRuntime())
	}
}
