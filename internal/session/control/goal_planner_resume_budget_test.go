package control

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/coordinator"
	"reasonix/internal/runtime/goaleval"
	"reasonix/internal/state/sessionstore"
)

type resumedPlannerRead struct{ calls atomic.Int32 }

func (*resumedPlannerRead) Name() string        { return "read_fixture" }
func (*resumedPlannerRead) Description() string { return "Read a fixture" }
func (*resumedPlannerRead) ReadOnly() bool      { return true }
func (*resumedPlannerRead) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (r *resumedPlannerRead) Execute(context.Context, json.RawMessage) (string, error) {
	return fmt.Sprintf("fixture observation %d", r.calls.Add(1)), nil
}

type resumedPlannerProvider struct{ calls atomic.Int32 }

func (*resumedPlannerProvider) Name() string { return "resumed-planner" }
func (p *resumedPlannerProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	n := p.calls.Add(1)
	ch := make(chan provider.Chunk, 3)
	if len(req.Tools) > 0 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("read-%d", n), Name: "read_fixture", Arguments: "{}",
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Continue with the verified fixture."}
	}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
		PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100, RequestCount: 1,
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestGoalResumeRenewsPlannerBudget(t *testing.T) {
	reads := &resumedPlannerRead{}
	registry := tool.NewRegistry()
	registry.Add(reads)
	planner, execution := &resumedPlannerProvider{}, &resumedPlannerProvider{}
	tee := NewGoalUsageTee(event.Discard)
	executor := agent.New(execution, registry, sessionstore.NewSession("executor"), agent.Options{}, tee)
	coord := coordinator.NewCoordinatorWithPlannerPolicy(planner, sessionstore.NewSession("planner"), nil,
		registry, agent.Options{}, executor, 0, tee, func(context.Context, string) agent.PlannerDecision {
			return agent.PlannerDecision{Route: agent.PlannerRoutePlanAndExecute, Depth: agent.PlannerDepthFull}
		})
	c := New(Options{Runner: coord, Executor: executor, Sink: tee, GoalTokenBudget: 3000,
		GoalEvaluator: &fakeGoalEvaluator{outcome: goaleval.OutcomeContinue, reason: "continue"}})
	t.Cleanup(c.Close)
	c.SetGoal("read the fixture until the investigation is complete")
	run := func() {
		t.Helper()
		if err := newTurnOrchestrator(c).runTurnLoop(goalReportedBudgetContext(t, t.Context()), orchestratedTurn{input: "continue", raw: "continue"}); err != nil {
			t.Fatal(err)
		}
		if c.GoalStatus() != GoalStatusBlocked || c.GoalRuntime().StopCause != stopCauseBudgetSpend {
			t.Fatalf("expected real budget pause: %+v", c.GoalRuntime())
		}
	}
	run()
	first := planner.calls.Load()
	if first != 4 {
		t.Fatalf("first planner requests=%d, want three reads plus finalization", first)
	}
	before := c.GoalRuntime()
	if !c.ResumeGoal() {
		t.Fatal("real spent-budget goal did not resume")
	}
	if after := c.GoalRuntime(); after.TokensUsed != before.TokensUsed || after.RequestsUsed != before.RequestsUsed {
		t.Fatal("resume erased observational usage")
	}
	run()
	if got := planner.calls.Load() - first; got != first {
		t.Fatalf("resumed planner requests=%d, want renewed slice %d; old local spend must not truncate research", got, first)
	}
}

func TestGoalNonBudgetResumeKeepsPlannerBudget(t *testing.T) {
	for _, mode := range []string{"manual_pause", "not_resumable"} {
		t.Run(mode, func(t *testing.T) {
			registry := tool.NewRegistry()
			registry.Add(&resumedPlannerRead{})
			prov := &resumedPlannerProvider{}
			executor := agent.New(&resumedPlannerProvider{}, registry, sessionstore.NewSession("executor"), agent.Options{}, event.Discard)
			coord := coordinator.NewCoordinator(prov, sessionstore.NewSession("planner"), nil, registry,
				agent.Options{}, executor, 0, event.Discard, nil)
			c := New(Options{Runner: coord, Executor: executor})
			t.Cleanup(c.Close)
			ctx := goalReportedBudgetContext(t, agent.WithTaskBudget(agent.WithDeliveryExecutionScope(t.Context(), agent.DeliveryExecutionScope{ID: "preserved"}), agent.TaskBudget{Tokens: 3000}))
			planner := coord.PlannerAgent()
			if _, ok := agent.InspectRunPause(planner.Run(ctx, "inspect the fixture")); !ok {
				t.Fatal("planner did not actually exhaust its local budget")
			}
			if mode == "manual_pause" {
				c.SetGoal("continue the investigation")
				if !c.PauseGoal() || !c.ResumeGoal() {
					t.Fatal("manual pause/resume did not transition")
				}
			} else if c.ResumeGoal() {
				t.Fatal("absent goal resumed")
			}
			before := prov.calls.Load()
			if _, ok := agent.InspectRunPause(planner.Run(ctx, "continue")); !ok {
				t.Fatal("unrenewed planner did not pause")
			}
			if got := prov.calls.Load() - before; got != 2 {
				t.Fatalf("non-budget resume reset planner spend: requests=%d want2", got)
			}
		})
	}
}
