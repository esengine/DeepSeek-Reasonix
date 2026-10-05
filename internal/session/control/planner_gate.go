package control

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/contract/agentpreset"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/taskpolicy"
)

// The planner gate answers one question: did the user ask for a plan? Twelve
// keyword tables used to score the answer out of vocabulary; the executor reads
// the same words with more context and plans for itself when a task needs it.

const (
	plannerLightResearchRounds = 2
	plannerFullResearchRounds  = 6
)

const (
	plannerReasonExplicitPlanMode   = "explicit_plan_mode"
	plannerReasonSynthetic          = "synthetic"
	plannerReasonSlash              = "slash_command"
	plannerReasonEmpty              = "empty_turn"
	plannerReasonUserPlanAndExecute = "user_plan_and_execute"
	plannerReasonExecutorOwns       = "executor_owns_the_turn"
)

type plannerTurnMetadata struct {
	UserText         string
	Synthetic        bool
	ExplicitPlanMode bool
	Policy           taskpolicy.TaskPolicy
	PolicySet        bool
}

type plannerTurnMetadataKey struct{}

func withPlannerTurnMetadata(ctx context.Context, meta plannerTurnMetadata) context.Context {
	return context.WithValue(ctx, plannerTurnMetadataKey{}, meta)
}

func plannerTurnMetadataFromContext(ctx context.Context) (plannerTurnMetadata, bool) {
	if ctx == nil {
		return plannerTurnMetadata{}, false
	}
	meta, ok := ctx.Value(plannerTurnMetadataKey{}).(plannerTurnMetadata)
	return meta, ok
}

func (c *Controller) withPlannerTurnMetadata(ctx context.Context, userText string, synthetic bool) context.Context {
	policy := taskpolicy.Derive(taskpolicy.Input{
		Preset:   agentpreset.Normalize(c.AgentPreset()),
		PlanMode: c.PlanMode(),
	})
	ctx = taskpolicy.WithContext(ctx, policy)
	return withPlannerTurnMetadata(ctx, plannerTurnMetadata{
		UserText:         userText,
		Synthetic:        synthetic,
		ExplicitPlanMode: c.PlanMode(),
		Policy:           policy,
		PolicySet:        true,
	})
}

// DecidePlannerRoute routes a turn to the two-model planner only when the user
// asked for a plan. It never calls a model and never scores the task text.
func DecidePlannerRoute(ctx context.Context, input string) agent.PlannerDecision {
	meta, hasMeta := plannerTurnMetadataFromContext(ctx)
	composedText := strings.TrimSpace(sessionstore.StripTransientUserBlocks(input))
	text := composedText
	if hasMeta && strings.TrimSpace(meta.UserText) != "" {
		text = strings.TrimSpace(meta.UserText)
	}

	if meta.ExplicitPlanMode || strings.HasPrefix(composedText, PlanModeMarker) {
		return plannerExecutorDecision(plannerReasonExplicitPlanMode)
	}
	if meta.Synthetic || IsSyntheticUserMessage(text) {
		return plannerExecutorDecision(plannerReasonSynthetic)
	}
	if text == "" {
		return plannerExecutorDecision(plannerReasonEmpty)
	}
	if strings.HasPrefix(text, "/") {
		return plannerExecutorDecision(plannerReasonSlash)
	}

	if strings.HasPrefix(composedText, PlannerRouteMarker) {
		return plannerPlanDecision(agent.PlannerRoutePlanAndExecute, agent.PlannerDepthFull, plannerReasonUserPlanAndExecute)
	}
	// Nobody asked for a plan. The executor decides for itself whether this
	// turn needs steps, and states them with todo_write where the delivery
	// gates and the contract can both read them.
	return plannerExecutorDecision(plannerReasonExecutorOwns)
}

func plannerExecutorDecision(reason string) agent.PlannerDecision {
	return agent.PlannerDecision{
		Route:  agent.PlannerRouteExecutorOnly,
		Depth:  agent.PlannerDepthNone,
		Reason: reason,
	}
}

func plannerPlanDecision(route agent.PlannerRoute, depth agent.PlannerDepth, reason string) agent.PlannerDecision {
	rounds := plannerLightResearchRounds
	if depth == agent.PlannerDepthFull {
		rounds = plannerFullResearchRounds
	}
	return agent.PlannerDecision{
		Route:             route,
		Depth:             depth,
		Reason:            reason,
		MaxResearchRounds: rounds,
	}
}

// NewPlannerPolicy returns the structured deterministic policy used by the
// two-model product path.
func NewPlannerPolicy() agent.PlannerPolicy {
	return DecidePlannerRoute
}

// NewPlannerGate retains the historical bool shape for direct callers.
func NewPlannerGate() func(context.Context, string) bool {
	return func(ctx context.Context, input string) bool {
		return DecidePlannerRoute(ctx, input).Route != agent.PlannerRouteExecutorOnly
	}
}
