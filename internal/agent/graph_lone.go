package agent

import (
	"context"
	"errors"
	"strings"

	"reasonix/internal/agentgraph"
	"reasonix/internal/event"
)

type declaredGraphNodeKey struct{}

// declaredGraphNode is the node a fan-out drew for one of its items: the sink
// it was drawn on and the id it was drawn under. Both travel, because the sink
// the item itself runs under is a nesting sink that forwards tool events and
// drops graph deltas — the picture is reachable only through the group's.
type declaredGraphNode struct {
	sink event.Sink
	id   string
}

// withDeclaredGraphNode marks a run whose graph node its caller already
// published. Only a fan-out can say this: it chose the node's id and kind, and
// a second declaration would redraw its worker as a group of one.
func withDeclaredGraphNode(ctx context.Context, sink event.Sink, id string) context.Context {
	return context.WithValue(ctx, declaredGraphNodeKey{}, declaredGraphNode{sink: sink, id: id})
}

func graphNodeDeclared(ctx context.Context) (declaredGraphNode, bool) {
	node, ok := ctx.Value(declaredGraphNodeKey{}).(declaredGraphNode)
	return node, ok
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
