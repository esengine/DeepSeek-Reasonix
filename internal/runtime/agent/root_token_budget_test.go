package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type tokenBudgetChildTool struct {
	readProbe
	child  *Agent
	err    error
	budget *TaskBudget
}

func (t *tokenBudgetChildTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if t.budget != nil {
		ctx = WithTaskBudget(ctx, *t.budget)
	}
	t.err = t.child.Run(ctx, "read a few files")
	return "child finished", nil
}

func TestRootTokenBudgetSurvivesNestedAndConcurrentChildren(t *testing.T) {
	for _, mode := range []string{"nested", "siblings", "child_override"} {
		t.Run(mode, func(t *testing.T) {
			leafProvider := &spendingProvider{max: 10}
			leafTools := tool.NewRegistry()
			leafTools.Add(readProbe{})
			leaf := New(leafProvider, leafTools, sessionstore.NewSession("leaf"), Options{}, event.Discard)
			call := &tokenBudgetChildTool{child: leaf}
			if mode == "child_override" {
				call.budget = &TaskBudget{}
			}
			rootTools := tool.NewRegistry()
			rootTools.Add(call)
			var siblings *tokenBudgetSiblings
			switch mode {
			case "nested":
				middle := New(&spendingProvider{max: 1}, rootTools, sessionstore.NewSession("middle"), Options{}, event.Discard)
				rootTools = tool.NewRegistry()
				rootTools.Add(&tokenBudgetChildTool{child: middle})
			case "siblings":
				otherTools := tool.NewRegistry()
				otherTools.Add(readProbe{})
				other := New(&spendingProvider{max: 10}, otherTools, sessionstore.NewSession("sibling"), Options{}, event.Discard)
				rootTools = tool.NewRegistry()
				siblings = &tokenBudgetSiblings{children: []*Agent{leaf, other}}
				rootTools.Add(siblings)
			}
			root := New(&spendingProvider{max: 1}, rootTools, sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Tokens: 1500}}, event.Discard)
			pause, ok := InspectRunPause(root.Run(t.Context(), "delegate work"))
			if !ok || pause.Kind != "task_budget" || pause.Key != "token" || !pause.HostOwned {
				t.Fatalf("root pause = %+v (%v), want token budget", pause, ok)
			}
			if leafProvider.rounds.Load() != 2 {
				t.Fatalf("leaf requests = %d, want one tool round and one existing finalization round", leafProvider.rounds.Load())
			}
			if siblings != nil {
				for i, err := range siblings.errors {
					pause, ok := InspectRunPause(err)
					if !ok || pause.Key != "token" || siblings.children[i].svc.prov.(*spendingProvider).rounds.Load() != 2 {
						t.Errorf("sibling%d did not execute and land on the shared token gate: %v", i, err)
					}
				}
			} else if pause, ok := InspectRunPause(call.err); !ok || pause.Key != "token" {
				t.Errorf("leaf did not pause on tokens: %v", call.err)
			}
		})
	}
}

func TestRootTokenBudgetIncludesChildProviderUsage(t *testing.T) {
	for _, test := range []struct {
		name       string
		configured int
		override   *TaskBudget
		wantChild  int32
		wantPause  bool
	}{
		{name: "configured", configured: 1500, wantChild: 2, wantPause: true},
		{name: "context", override: &TaskBudget{Tokens: 3000}, wantChild: 3, wantPause: true},
		{name: "unconfigured", wantChild: 11},
		{name: "explicit_zero", configured: 1500, override: &TaskBudget{}, wantChild: 11},
	} {
		t.Run(test.name, func(t *testing.T) {
			childProvider := &spendingProvider{max: 10}
			childTools := tool.NewRegistry()
			childTools.Add(readProbe{})
			child := New(childProvider, childTools, sessionstore.NewSession("child"), Options{}, event.Discard)
			call := &tokenBudgetChildTool{child: child}
			parentTools := tool.NewRegistry()
			parentTools.Add(call)
			parent := New(&spendingProvider{max: 1}, parentTools, sessionstore.NewSession("parent"), Options{TaskBudget: TaskBudget{Tokens: test.configured}}, event.Discard)
			ctx := t.Context()
			if test.override != nil {
				ctx = WithTaskBudget(ctx, *test.override)
			}
			err := parent.Run(ctx, "delegate a read")
			if got := childProvider.rounds.Load(); got != test.wantChild {
				t.Errorf("child provider requests = %d, want %d including its existing finalization round", got, test.wantChild)
			}
			for role, runErr := range map[string]error{"parent": err, "child": call.err} {
				info, paused := InspectRunPause(runErr)
				if test.wantPause {
					if !paused || info.Kind != "task_budget" || info.Key != "token" || !info.HostOwned {
						t.Errorf("%s pause = %+v (%v), want host-owned token pause", role, info, runErr)
					}
				} else if runErr != nil {
					t.Errorf("%s run: %v", role, runErr)
				}
			}
		})
	}
}
