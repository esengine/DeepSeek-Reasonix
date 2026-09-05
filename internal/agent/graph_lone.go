package agent

import (
	"context"
	"errors"
	"strings"

	"reasonix/internal/agentgraph"
)

type declaredGraphNodeKey struct{}

// withDeclaredGraphNode marks a run whose graph node its caller already
// published. Only a fan-out can say this: it chose the node's id and kind, and
// a second declaration would redraw its worker as a group of one.
func withDeclaredGraphNode(ctx context.Context) context.Context {
	return context.WithValue(ctx, declaredGraphNodeKey{}, true)
}

func graphNodeDeclared(ctx context.Context) bool {
	declared, _ := ctx.Value(declaredGraphNodeKey{}).(bool)
	return declared
}

// delegationState reads one run's ending the way a fan-out reads its items', so
// a lone worker and a fleet member cannot disagree about what cancelled means.
func delegationState(ctx context.Context, err error) agentgraph.NodeState {
	switch {
	case err == nil:
		return agentgraph.StateCompleted
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), ctx.Err() != nil:
		return agentgraph.StateCancelled
	default:
		return agentgraph.StateFailed
	}
}

// delegationLabel names the lane: what was delegated to, which is the profile
// when there is one and the worker's own kind otherwise.
func delegationLabel(spec ProfileExecSpec) string {
	if name := strings.TrimSpace(spec.Worker.Profile); name != "" {
		return name
	}
	if name := strings.TrimSpace(spec.Worker.Name); name != "" {
		return name
	}
	return "task"
}

// delegationTaskLabel names the card: what this run was asked to do.
func delegationTaskLabel(spec ProfileExecSpec) string {
	if d := strings.TrimSpace(spec.Task.Description); d != "" {
		return d
	}
	return boundedInline(strings.TrimSpace(spec.Task.Objective), 80)
}
