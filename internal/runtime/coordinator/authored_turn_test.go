package coordinator

import (
	"context"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"testing"
)

// TestPlannerNeverNamesTheTurnAtTheParentSink keeps the planner's own turn
// identity out of the conversation's: it runs over its own session, so a start
// it announces names a message in that transcript. Under a host boundary it
// announces nothing; standalone it still does, which is why the coordinator's
// sink filter is not dead code.
func TestPlannerNeverNamesTheTurnAtTheParentSink(t *testing.T) {
	for _, hosted := range []bool{false, true} {
		name := "standalone"
		if hosted {
			name = "host owns the boundary"
		}
		t.Run(name, func(t *testing.T) {
			var starts []event.Event
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.TurnStarted {
					starts = append(starts, e)
				}
			})
			// The planner's transcript is longer, so a name minted over it
			// cannot be mistaken for one minted over the executor's.
			plannerSession := sessionstore.NewSession("planner system")
			plannerSession.Add(provider.Message{Role: provider.RoleUser, Content: "earlier planner turn"})
			plannerSession.Add(provider.Message{Role: provider.RoleAssistant, Content: "earlier plan"})
			exec := agent.New(&turnStartProvider{}, tool.NewRegistry(), sessionstore.NewSession("system"), agent.Options{}, sink)
			c := NewCoordinatorWithPlannerPolicy(
				&turnStartProvider{}, plannerSession, nil, tool.NewRegistry(), agent.Options{}, exec, 0, sink,
				func(context.Context, string) agent.PlannerDecision {
					return agent.PlannerDecision{Route: agent.PlannerRoutePlanAndExecute, Depth: agent.PlannerDepthFull, Reason: "test"}
				},
			)
			ctx := context.Background()
			if hosted {
				ctx = agent.WithHostTurnBoundary(ctx, agent.HostTurnBoundary{})
			}
			if err := c.Run(ctx, "第一句用户输入"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			named := 0
			for _, e := range starts {
				if e.AuthoredTurn == nil {
					continue
				}
				named++
				if *e.MsgIndex != 1 {
					t.Fatalf("a start named message %d; the executor's turn is message 1, the planner's is not this turn",
						*e.MsgIndex)
				}
			}
			want := 1
			if hosted {
				want = 0
			}
			if named != want {
				t.Fatalf("named starts at the parent sink = %d, want %d", named, want)
			}
		})
	}
}
