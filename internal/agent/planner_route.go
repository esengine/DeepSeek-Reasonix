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

// PlannerIntent is the explicit planner request for one turn. Ordinary work is
// always executor-only; the planner never infers complexity from wording.
type PlannerIntent = PlannerRoute

// PlannerDecision is the deterministic, host-owned routing result for one turn.
// Reason is an opaque privacy-safe code for diagnostics; user text never belongs
// in it.
type PlannerDecision struct {
	Route  PlannerRoute
	Reason string
}

// PlannerPolicy makes one deterministic routing decision from trusted turn
// context plus the composed model input.
type PlannerPolicy func(context.Context, string) PlannerDecision

func normalizePlannerDecision(d PlannerDecision) PlannerDecision {
	switch d.Route {
	case PlannerRouteExecutorOnly, PlannerRoutePlanAndExecute, PlannerRoutePlanForApproval, PlannerRoutePlanOnly:
	default:
		d.Route = PlannerRouteExecutorOnly
	}
	d.Reason = strings.TrimSpace(d.Reason)
	if d.Reason == "" {
		d.Reason = "default"
	}
	return d
}
