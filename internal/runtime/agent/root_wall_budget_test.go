package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type wallBudgetDelayTool struct {
	readProbe
	next tool.Tool
}

func (t *wallBudgetDelayTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return t.next.Execute(ctx, args)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestRootWallBudgetReachesDelegatedAgents(t *testing.T) {
	for _, mode := range []string{"foreground", "nested", "siblings", "context", "unconfigured", "explicit_zero"} {
		t.Run(mode, func(t *testing.T) {
			leafProvider := &spendingProvider{max: 10}
			leafTools := tool.NewRegistry()
			leafTools.Add(readProbe{})
			leaf := New(leafProvider, leafTools, sessionstore.NewSession("leaf"), Options{}, event.Discard)
			call := &tokenBudgetChildTool{child: leaf, budget: &TaskBudget{}}
			var delegate tool.Tool = call
			var siblings *tokenBudgetSiblings
			switch mode {
			case "nested":
				middleTools := tool.NewRegistry()
				middleTools.Add(call)
				middle := New(&spendingProvider{max: 1}, middleTools, sessionstore.NewSession("middle"), Options{}, event.Discard)
				delegate = &tokenBudgetChildTool{child: middle}
			case "siblings":
				otherTools := tool.NewRegistry()
				otherTools.Add(readProbe{})
				other := New(&spendingProvider{max: 10}, otherTools, sessionstore.NewSession("sibling"), Options{}, event.Discard)
				siblings = &tokenBudgetSiblings{children: []*Agent{leaf, other}}
				delegate = siblings
			}
			rootTools := tool.NewRegistry()
			rootTools.Add(&wallBudgetDelayTool{next: delegate})
			limit := TaskBudget{Wall: time.Nanosecond}
			if mode == "unconfigured" || mode == "context" {
				limit = TaskBudget{}
			}
			root := New(&spendingProvider{max: 1}, rootTools, sessionstore.NewSession("root"), Options{TaskBudget: limit}, event.Discard)
			ctx := t.Context()
			switch mode {
			case "context":
				ctx = WithTaskBudget(ctx, TaskBudget{Wall: time.Nanosecond})
			case "explicit_zero":
				ctx = WithTaskBudget(ctx, TaskBudget{})
			}
			err := root.Run(ctx, "delegate reads")
			bounded := mode != "unconfigured" && mode != "explicit_zero"
			want := int32(11)
			if bounded {
				want = 2
			}
			if leafProvider.rounds.Load() != want {
				t.Fatalf("leaf requests = %d, want %d including its existing finalization round", leafProvider.rounds.Load(), want)
			}
			if !bounded {
				if err != nil || call.err != nil {
					t.Fatalf("disabled budget stopped root=%v child=%v", err, call.err)
				}
				return
			}
			pause, ok := InspectRunPause(err)
			if !ok || pause.Kind != "task_budget" || pause.Key != "time" || !pause.HostOwned {
				t.Fatalf("root pause = %+v (%v), want host-owned time budget", pause, err)
			}
			if siblings != nil {
				for i, childErr := range siblings.errors {
					pause, ok := InspectRunPause(childErr)
					if !ok || pause.Key != "time" || siblings.children[i].svc.prov.(*spendingProvider).rounds.Load() != 2 {
						t.Fatalf("sibling%d did not land on root time: %v", i, childErr)
					}
				}
			} else if pause, ok := InspectRunPause(call.err); !ok || pause.Key != "time" {
				t.Fatalf("leaf pause = %+v (%v), want root time", pause, call.err)
			}
		})
	}
}
