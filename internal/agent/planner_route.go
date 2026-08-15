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

// PlannerDepth controls how much evidence and detail the planning contract asks
// for. It deliberately does not impose a model-round budget: planning lands on
// submit_plan and the shared adaptive progress, task-budget, and context guards
// own runaway protection. None is only valid for ExecutorOnly.
type PlannerDepth string

const (
	PlannerDepthNone  PlannerDepth = "none"
	PlannerDepthLight PlannerDepth = "light"
	PlannerDepthFull  PlannerDepth = "full"
)

// PlannerDecision is the deterministic, host-owned routing result for one turn.
// Reason is an opaque privacy-safe code for diagnostics; user text never belongs
// in it.
type PlannerDecision struct {
	Route  PlannerRoute
	Depth  PlannerDepth
	Reason string
}

// PlannerPolicy makes one deterministic routing decision from trusted turn
// context plus the composed model input.
type PlannerPolicy func(context.Context, string) PlannerDecision

func normalizePlannerDecision(d PlannerDecision) PlannerDecision {
	switch d.Route {
	case PlannerRouteExecutorOnly:
		d.Depth = PlannerDepthNone
	case PlannerRoutePlanAndExecute, PlannerRoutePlanForApproval, PlannerRoutePlanOnly:
		if d.Depth != PlannerDepthLight && d.Depth != PlannerDepthFull {
			d.Depth = PlannerDepthFull
		}
	default:
		d.Route = PlannerRoutePlanAndExecute
		d.Depth = PlannerDepthFull
	}
	d.Reason = strings.TrimSpace(d.Reason)
	if d.Reason == "" {
		d.Reason = "default"
	}
	return d
}
