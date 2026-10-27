package control

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/coordinator"
	"reasonix/internal/runtime/goaleval"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func TestGoalFailedLedgerRenewalPreservesPlannerSpendAndGoalState(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Add(&resumedPlannerRead{})
	planner := &resumedPlannerProvider{}
	tee := NewGoalUsageTee(event.Discard)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	executor := agent.New(&resumedPlannerProvider{}, registry, sessionstore.NewSession("executor"), agent.Options{SessionPath: path}, tee)
	coord := coordinator.NewCoordinatorWithPlannerPolicy(planner, sessionstore.NewSession("planner"), nil,
		registry, agent.Options{}, executor, 0, tee, func(context.Context, string) agent.PlannerDecision {
			return agent.PlannerDecision{Route: agent.PlannerRoutePlanAndExecute, Depth: agent.PlannerDepthFull}
		})
	c := New(Options{Runner: coord, Executor: executor, Sink: tee, GoalTokenBudget: 3000,
		GoalEvaluator: &fakeGoalEvaluator{outcome: goaleval.OutcomeContinue, reason: "continue"}})
	t.Cleanup(c.Close)
	c.SetSessionPath(path)
	c.SetGoal("investigate the fixture")
	scope, _, _ := c.goals.deliveryScope()
	if err := newTurnOrchestrator(c).runTurnLoop(goalReportedBudgetContext(t, t.Context()), orchestratedTurn{input: "continue", raw: "continue"}); err != nil {
		t.Fatal(err)
	}
	before := c.GoalRuntime()
	if before.StopCause != stopCauseBudgetSpend || planner.calls.Load() != 4 {
		t.Fatalf("fixture did not exhaust planner: %+v calls=%d", before, planner.calls.Load())
	}
	dir := store.SessionBudgetDir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(dir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(scope))))
	data := []byte(`{"version":999}`)
	if err := os.WriteFile(ledger, data, 0600); err != nil {
		t.Fatal(err)
	}
	if c.ResumeGoal() {
		t.Fatal("unknown ledger was renewed")
	}
	if after := c.GoalRuntime(); after != before {
		t.Fatalf("failed renewal changed goal: before=%+v after=%+v", before, after)
	}
	if got, err := os.ReadFile(ledger); err != nil || string(got) != string(data) {
		t.Fatal("unknown ledger changed", err)
	}
	ctx := goalReportedBudgetContext(t, agent.WithTaskBudget(agent.WithDeliveryExecutionScope(t.Context(), agent.DeliveryExecutionScope{ID: scope}), agent.TaskBudget{Tokens: 3000}))
	count := planner.calls.Load()
	if _, ok := agent.InspectRunPause(coord.PlannerAgent().Run(ctx, "continue")); !ok {
		t.Fatal("unrenewed planner did not pause")
	}
	if got := planner.calls.Load() - count; got != 2 {
		t.Fatalf("failed ledger renewal reset planner: requests=%d want2", got)
	}
}
