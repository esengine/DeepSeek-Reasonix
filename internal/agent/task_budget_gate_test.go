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

// No amount of money is portable across models: a default loose enough for a
// cheap model lands a frontier model within a couple of answers. Only wall
// clock, which means the same everywhere, ships with a default.
func TestTaskBudgetShipsNoCostDefault(t *testing.T) {
	got := taskBudgetOrDefault(TaskBudget{})
	if got.Cost != 0 {
		t.Fatalf("default cost = %v, want it off until the user sets it", got.Cost)
	}
	if got.Wall != DefaultTaskWall {
		t.Fatalf("default wall = %v, want %v", got.Wall, DefaultTaskWall)
	}
}

func TestTaskBudgetAxesDisableIndependently(t *testing.T) {
	got := taskBudgetOrDefault(TaskBudget{Cost: 1.5, Wall: -1})
	if got.Cost != 1.5 || got.Wall != 0 {
		t.Fatalf("budget = %+v, want the explicit cost kept and wall clock disabled", got)
	}
	// Setting cost alone must not drop the wall-clock default: that is the
	// axis an unpriced or free model still needs.
	if got := taskBudgetOrDefault(TaskBudget{Cost: 1.5}); got.Wall != DefaultTaskWall {
		t.Fatalf("budget = %+v, want the wall default kept alongside an explicit cost", got)
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
