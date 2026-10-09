package delegation

import (
	"encoding/json"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestGlobalEffortDefaultIsResolvedForTheModelThatRuns(t *testing.T) {
	inherited := func(model string) string {
		if model == "narrow" {
			return ""
		}
		return "max"
	}
	build := func(opts TaskToolOptions) *TaskTool {
		opts.Provider = &mockProvider{name: "sub"}
		opts.ParentRegistry = tool.NewRegistry()
		opts.MaxSteps = 5
		opts.InheritedEffort = inherited
		return NewTaskToolWithOptions(opts)
	}
	profile := func(task *TaskTool, args map[string]string) (string, string) {
		raw, _ := json.Marshal(args)
		p := task.ResolveProfile(raw)
		if p == nil {
			t.Fatal("no profile")
		}
		return p.Model, p.Effort
	}

	task := build(TaskToolOptions{})
	if _, eff := profile(task, nil); eff != "max" {
		t.Fatalf("parent effort = %q, want max", eff)
	}
	if _, eff := profile(task, map[string]string{"model": "narrow"}); eff != "" {
		t.Fatalf("call-selected narrow model effort = %q, want the global default dropped", eff)
	}
	if _, eff := profile(task, map[string]string{"model": "narrow", "effort": "low"}); eff != "low" {
		t.Fatalf("explicit call effort = %q, want low", eff)
	}

	task = build(TaskToolOptions{SubagentModel: "narrow"})
	if model, eff := profile(task, nil); model != "narrow" || eff != "" {
		t.Fatalf("task model default = %q/%q, want narrow with the default dropped", model, eff)
	}

	task = build(TaskToolOptions{SubagentModel: "narrow", SubagentEffort: "high"})
	if _, eff := profile(task, nil); eff != "high" {
		t.Fatalf("explicit task effort = %q, want high to win over the inherited default", eff)
	}
}
