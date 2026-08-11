package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// The gate must stop a runaway on the axis it actually runs away on. This
// provider never repeats itself and never fails, so every adaptive guard stays
// quiet — the shape that burned four hours — and only spend can catch it.
func TestTaskBudgetGateLandsARunawayOnCost(t *testing.T) {
	sink := newBudgetSink()
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	pricing := &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "CNY"}
	// Each round bills 900 hits + 100 misses + 100 output = 3.18e-4.
	// A 1e-3 budget lands on the fourth round; 500 rounds are available.
	a := New(&spendingProvider{max: 500}, reg, NewSession("sys"),
		Options{Pricing: pricing, TaskBudget: TaskBudget{Cost: 1e-3}}, sink)

	err := a.Run(context.Background(), "read everything")
	if err == nil {
		t.Fatal("Run returned nil; want a resumable task-budget pause")
	}
	info, ok := InspectRunPause(err)
	if !ok || info.Kind != "task_budget" || info.Key != "cost" {
		t.Fatalf("pause = %+v (%v), want a host-owned task_budget pause on cost", info, err)
	}
	if !info.HostOwned {
		t.Fatal("a host-imposed budget must report HostOwned")
	}
	last := sink.samples[len(sink.samples)-1]
	if last.Task.Rounds > 20 {
		t.Fatalf("ran %d rounds before landing; the gate should fire on spend, not drift", last.Task.Rounds)
	}
	if last.Task.Cost < 1e-3 {
		t.Fatalf("task cost %v landed below its own budget", last.Task.Cost)
	}
}

// Landing is one tool-free summary, not a truncated turn: the work stays in
// the session and the next message continues it.
func TestTaskBudgetGateKeepsTheWorkAndAsksForASummary(t *testing.T) {
	sink := newBudgetSink()
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	a := New(&spendingProvider{max: 500}, reg, NewSession("sys"),
		Options{
			Pricing:    &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2},
			TaskBudget: TaskBudget{Cost: 1e-3},
		}, sink)

	_ = a.Run(context.Background(), "read everything")

	var sawNudge, sawToolResult bool
	for _, m := range a.session.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "reached its cost budget") {
			sawNudge = true
		}
		if m.Role == provider.RoleTool {
			sawToolResult = true
		}
	}
	if !sawNudge {
		t.Fatal("no finalization request in the session; the model was cut off instead of asked to land")
	}
	if !sawToolResult {
		t.Fatal("completed tool work was dropped from the session")
	}
}

// An unpriced model must not read as free-and-therefore-fine, nor as instantly
// over budget. Cost simply does not gate it; wall clock still can.
func TestTaskBudgetGateIgnoresCostWhenUnpriced(t *testing.T) {
	var b runBudget
	b.observe(&provider.Usage{PromptTokens: 10_000_000, CompletionTokens: 1_000_000, RequestCount: 1}, nil)
	if axis, _ := b.exceeded(TaskBudget{Cost: 1e-9}); axis != "" {
		t.Fatalf("unpriced turn crossed the %q axis; cost cannot judge what it cannot price", axis)
	}
}

func TestTaskBudgetGateFiresOnWallClock(t *testing.T) {
	b := runBudget{started: time.Now().Add(-2 * time.Hour)}
	b.observe(&provider.Usage{PromptTokens: 1, RequestCount: 1}, nil)
	axis, detail := b.exceeded(TaskBudget{Wall: time.Hour})
	if axis != "time" {
		t.Fatalf("axis = %q, want the wall-clock crossing", axis)
	}
	if !strings.Contains(detail, "past the 1h0m0s budget") {
		t.Fatalf("detail = %q, want it to name the budget it crossed", detail)
	}
}

// Nothing is bounded out of the box. Stopping a task is the user's call: only
// they know which model they are paying for, and whether a long task is a
// runaway or the job they asked for.
func TestTaskBudgetShipsNoLimits(t *testing.T) {
	if got := normalizeTaskBudget(TaskBudget{}); got.Cost != 0 || got.Wall != 0 {
		t.Fatalf("default budget = %+v, want both axes off", got)
	}
}

func TestTaskBudgetAxesSetIndependently(t *testing.T) {
	got := normalizeTaskBudget(TaskBudget{Cost: 1.5, Wall: -1})
	if got.Cost != 1.5 || got.Wall != 0 {
		t.Fatalf("budget = %+v, want the explicit cost kept and wall clock off", got)
	}
	if got := normalizeTaskBudget(TaskBudget{Wall: time.Minute}); got.Wall != time.Minute || got.Cost != 0 {
		t.Fatalf("budget = %+v, want the explicit wall kept and cost off", got)
	}
}

// However long an unconfigured task runs and however much it spends, nothing
// lands it. This is the promise the defaults make.
func TestUnconfiguredBudgetNeverCrosses(t *testing.T) {
	b := runBudget{started: time.Now().Add(-8 * time.Hour)}
	b.observe(&provider.Usage{PromptTokens: 50_000_000, CompletionTokens: 5_000_000, RequestCount: 1},
		&provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2})
	if axis, detail := b.exceeded(normalizeTaskBudget(TaskBudget{})); axis != "" {
		t.Fatalf("unconfigured budget crossed %q (%s); nothing should stop by default", axis, detail)
	}
}

// An explicit max_steps still bounds a run by rounds when the user asks for
// that specifically, with every spend axis disabled.
func TestExplicitMaxStepsStillLandsWhenNoBudgetApplies(t *testing.T) {
	sink := event.FuncSink(func(event.Event) {})
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	a := New(&spendingProvider{max: 500}, reg, NewSession("sys"),
		Options{MaxSteps: 3, TaskBudget: TaskBudget{Cost: 0, Wall: -1}}, sink)

	err := a.Run(context.Background(), "read everything")
	var pause *maxStepsPause
	if !errors.As(err, &pause) {
		t.Fatalf("err = %v, want an explicit max_steps to still stop an unbudgeted runaway", err)
	}
}
