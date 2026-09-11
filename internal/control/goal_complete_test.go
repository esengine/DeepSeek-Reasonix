package control

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
)

func TestRepeatedCompleteWithOnlyProjectCheckFinishes(t *testing.T) {
	g := &goalMachine{goal: "audit the tree", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns}
	in := goalAdvanceInput{
		report: &goalTurnReport{status: GoalStatusComplete},
		readiness: agent.ReadinessResult{
			Ready:   false,
			Missing: []string{"project_check"},
			Reason:  `run "godot --headless -s e2e.gd" from AGENTS.md:4 after the latest write`,
		},
	}
	first := g.advance(in)
	if !first.cont || g.status != GoalStatusRunning {
		t.Fatalf("first complete should continue: result=%+v status=%s", first, g.status)
	}
	if !strings.Contains(first.intercept, "unverified") {
		t.Fatalf("first intercept = %q, want the unverified escape hatch", first.intercept)
	}
	second := g.advance(in)
	if second.cont || g.status != GoalStatusComplete || second.notice != goalCompleteNotice {
		t.Fatalf("second identical check-only complete should finish: result=%+v status=%s", second, g.status)
	}
}

func TestRepeatedCompleteWithOnlyVerificationFinishes(t *testing.T) {
	g := &goalMachine{goal: "audit the tree", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns}
	in := goalAdvanceInput{
		report: &goalTurnReport{status: GoalStatusComplete},
		readiness: agent.ReadinessResult{
			Ready:   false,
			Missing: []string{"verification"},
			Reason:  "run a relevant verification command after the latest write for the current role setting",
		},
	}
	if first := g.advance(in); !first.cont || g.status != GoalStatusRunning {
		t.Fatalf("first complete should continue: result=%+v status=%s", first, g.status)
	}
	if second := g.advance(in); second.cont || g.status != GoalStatusComplete {
		t.Fatalf("second identical verification gap should finish: result=%+v status=%s", second, g.status)
	}
}

func TestRejectedCompleteDoesNotCountAsProgress(t *testing.T) {
	g := &goalMachine{goal: "audit the tree", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns}
	in := goalAdvanceInput{
		report:    &goalTurnReport{status: GoalStatusComplete},
		readiness: agent.ReadinessResult{Ready: false, Missing: []string{"verification"}, Reason: "run a relevant verification command"},
	}
	g.advance(in)
	if g.noProgressTurns != 1 {
		t.Fatalf("rejected complete noProgressTurns = %d, want 1", g.noProgressTurns)
	}
}
