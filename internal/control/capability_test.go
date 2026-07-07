package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type capabilityRecordingRunner struct {
	input string
}

func (r *capabilityRecordingRunner) Run(_ context.Context, input string) error {
	r.input = input
	return nil
}

type capabilityTestTool struct{ name string }

func (t capabilityTestTool) Name() string { return t.name }
func (t capabilityTestTool) Description() string {
	return "test tool"
}
func (t capabilityTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t capabilityTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}
func (t capabilityTestTool) ReadOnly() bool { return true }

func TestRunInjectsCapabilityRouteForRelevantSkill(t *testing.T) {
	runner := &capabilityRecordingRunner{}
	reg := tool.NewRegistry()
	reg.Add(capabilityTestTool{name: "run_skill"})
	c := New(Options{
		Runner: runner,
		Skills: []skill.Skill{{
			Name:        "review",
			Description: "review code",
			Scope:       skill.ScopeBuiltin,
		}},
		Registry: reg,
	})

	if err := c.Run(context.Background(), "帮我看看这段代码有没有问题"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(runner.input, `<capability-route version="1">`) ||
		!strings.Contains(runner.input, "skill:review prefer") {
		t.Fatalf("input missing capability route:\n%s", runner.input)
	}
	if got := StripComposePrefixes(runner.input); got != "帮我看看这段代码有没有问题" {
		t.Fatalf("StripComposePrefixes = %q", got)
	}
}
