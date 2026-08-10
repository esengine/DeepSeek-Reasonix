package agent

import (
	"context"
	"strings"
)

// PlannerRoute describes how a two-model turn should flow. It is deliberately
// separate from explicit Plan Mode: ExecutorOnly lets the ordinary executor
// handle that host-owned workflow without invoking a second planner.
type PlannerRoute string

const (
	PlannerRouteExecutorOnly    PlannerRoute = "executor_only"
	PlannerRoutePlanAndExecute  PlannerRoute = "plan_and_execute"
	PlannerRoutePlanForApproval PlannerRoute = "plan_for_approval"
	PlannerRoutePlanOnly        PlannerRoute = "plan_only"
)

// PlannerDepth controls the planning contract and research budget. None is only
// valid for ExecutorOnly.
type PlannerDepth string

const (
	PlannerDepthNone  PlannerDepth = "none"
	PlannerDepthLight PlannerDepth = "light"
	PlannerDepthFull  PlannerDepth = "full"
)

// PlannerDecision is the deterministic, host-owned routing result for one turn.
// Reason is an opaque privacy-safe code for diagnostics; user text never belongs
// in it. MaxResearchRounds bounds planner tool-call rounds for this turn only.
type PlannerDecision struct {
	Route             PlannerRoute
	Depth             PlannerDepth
	Reason            string
	MaxResearchRounds int
}

// PlannerPolicy makes one deterministic routing decision from trusted turn
// context plus the composed model input.
type PlannerPolicy func(context.Context, string) PlannerDecision

func normalizePlannerDecision(d PlannerDecision) PlannerDecision {
	switch d.Route {
	case PlannerRouteExecutorOnly:
		d.Depth = PlannerDepthNone
		d.MaxResearchRounds = 0
	case PlannerRoutePlanAndExecute, PlannerRoutePlanForApproval, PlannerRoutePlanOnly:
		if d.Depth != PlannerDepthLight && d.Depth != PlannerDepthFull {
			d.Depth = PlannerDepthFull
		}
		if d.MaxResearchRounds < 0 {
			d.MaxResearchRounds = 0
		}
	default:
		d.Route = PlannerRoutePlanAndExecute
		d.Depth = PlannerDepthFull
		d.MaxResearchRounds = 0
	}
	d.Reason = strings.TrimSpace(d.Reason)
	if d.Reason == "" {
		d.Reason = "default"
	}
	return d
}

type runStepLimit struct {
	steps           int
	key             string
	defaultOnly     bool
	inherit         bool
	pauseAfterFinal bool
}

type runStepLimitContextKey struct{}

func withRunStepLimit(ctx context.Context, steps int, key string) context.Context {
	if steps <= 0 {
		return ctx
	}
	return context.WithValue(ctx, runStepLimitContextKey{}, runStepLimit{
		steps: steps,
		key:   strings.TrimSpace(key),
	})
}

// WithDefaultRunStepLimit installs a host-owned zero-configuration Run limit.
// It applies only when the Agent has no explicit MaxSteps configuration and is
// inherited by delegated sub-agents as their effective parent budget. Planner
// limits continue to use withRunStepLimit, which is an unconditional boundary
// and deliberately does not leak into executor children.
func WithDefaultRunStepLimit(ctx context.Context, steps int, key string) context.Context {
	if steps <= 0 {
		return ctx
	}
	return context.WithValue(ctx, runStepLimitContextKey{}, runStepLimit{
		steps:           steps,
		key:             strings.TrimSpace(key),
		defaultOnly:     true,
		inherit:         true,
		pauseAfterFinal: true,
	})
}

func runStepLimitFromContext(ctx context.Context) (runStepLimit, bool) {
	if ctx == nil {
		return runStepLimit{}, false
	}
	limit, ok := ctx.Value(runStepLimitContextKey{}).(runStepLimit)
	return limit, ok && limit.steps > 0
}
