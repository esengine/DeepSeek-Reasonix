package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/skill"
)

// The arm is only a control if it removes delegation whichever tool reaches it.
// A measured no-subagent run spent a child on `explore` before this gate.
func TestSubagentArmRefusesProfileSkillDelegation(t *testing.T) {
	called := false
	run := skill.SubagentRunner(func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
		called = true
		return "ran", nil
	})
	sk := skill.Skill{Name: "explore", RunAs: skill.RunSubagent}

	gated := gateSubagentArm(ablation.New(ablation.Subagent), run)
	if _, err := gated(context.Background(), sk, "task", skill.SubagentRunOptions{}); err == nil ||
		!strings.Contains(err.Error(), "subagent ablation arm") {
		t.Fatalf("err = %v, want a refusal naming the arm", err)
	}
	if called {
		t.Fatal("the gate must refuse before spawning a child")
	}

	// Without the arm the runner is untouched, so the normal path keeps its
	// exact behaviour rather than routing through a wrapper.
	if _, err := gateSubagentArm(ablation.Set{}, run)(context.Background(), sk, "task", skill.SubagentRunOptions{}); err != nil {
		t.Fatalf("ungated run: %v", err)
	}
	if !called {
		t.Fatal("the ungated runner must still run")
	}
}

// Under the arm the control run must not even carry the wrapper schemas.
func TestSubagentArmDropsBuiltinWrapperTools(t *testing.T) {
	run := skill.SubagentRunner(func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
		return "", nil
	})
	if got := builtinSubagentTools(ablation.New(ablation.Subagent), skill.New(skill.Options{}), run); len(got) != 0 {
		t.Fatalf("arm still registered %d wrapper tools", len(got))
	}
	if got := builtinSubagentTools(ablation.Set{}, skill.New(skill.Options{}), run); len(got) == 0 {
		t.Fatal("the ordinary run must keep explore/research/review")
	}
}
