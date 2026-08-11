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
