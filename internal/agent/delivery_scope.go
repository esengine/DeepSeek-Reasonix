package agent

import (
	"context"
	"strings"
)

// DeliveryExecutionScope is host-owned state for a multi-turn task. It never
// enters the provider request; it only controls delivery evidence lifetime and
// trusted task classification across synthetic continuation turns.
type DeliveryExecutionScope struct {
	ID       string
	TaskText string
}

type deliveryExecutionScopeKey struct{}

func WithDeliveryExecutionScope(ctx context.Context, scope DeliveryExecutionScope) context.Context {
	scope.ID = strings.TrimSpace(scope.ID)
	scope.TaskText = strings.TrimSpace(scope.TaskText)
	if scope.ID == "" {
		return ctx
	}
	return context.WithValue(ctx, deliveryExecutionScopeKey{}, scope)
}

func DeliveryExecutionScopeFromContext(ctx context.Context) (DeliveryExecutionScope, bool) {
	if ctx == nil {
		return DeliveryExecutionScope{}, false
	}
	scope, ok := ctx.Value(deliveryExecutionScopeKey{}).(DeliveryExecutionScope)
	if !ok || strings.TrimSpace(scope.ID) == "" {
		return DeliveryExecutionScope{}, false
	}
	return scope, true
}

type deliveryGoalDisposition int

const (
	deliveryGoalFinal deliveryGoalDisposition = iota
	deliveryGoalContinue
	deliveryGoalBlocked
)

func deliveryDisposition(text string) deliveryGoalDisposition {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.ToLower(strings.TrimSpace(lines[i]))
		if line == "" {
			continue
		}
		switch {
		case line == "[goal:continue]":
			return deliveryGoalContinue
		case strings.HasPrefix(line, "[goal:blocked:") && strings.HasSuffix(line, "]"):
			return deliveryGoalBlocked
		default:
			return deliveryGoalFinal
		}
	}
	return deliveryGoalFinal
}
