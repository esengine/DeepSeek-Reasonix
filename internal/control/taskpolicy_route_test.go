package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestTurnOrchestratorRoutesCapabilitiesWithFrozenTaskPolicy(t *testing.T) {
	runner := &plannerMetadataRunner{}
	reg := tool.NewRegistry()
	reg.Add(capabilityTestTool{name: "run_skill"})
	c := New(Options{
		Runner:   runner,
		Registry: reg,
		Skills: []skill.Skill{{
			Name:     "security-review",
			Scope:    skill.ScopeBuiltin,
			Triggers: []string{"authentication"},
			AutoUse:  "suggest",
		}},
	})

	const raw = "fix the authentication bypass"
	if err := newTurnOrchestrator(c).runTurnWithRawDisplay(context.Background(), raw, raw, ""); err != nil {
		t.Fatal(err)
	}
	if !runner.meta.PolicySet || !runner.meta.Policy.ClosedLoop() {
		t.Fatalf("runner policy = %+v, want frozen closed-loop policy", runner.meta)
	}
	if !strings.Contains(runner.input, "skill:security-review prefer") {
		t.Fatalf("interactive capability route did not consume the frozen policy:\n%s", runner.input)
	}
}
