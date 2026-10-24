package agent

import (
	"context"
	"crypto/rand"
)

type nextExecutionKey struct{}
type executionKey struct{}

// WithRunExecution binds the next Agent.Run to its host-owned execution record.
func WithRunExecution(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, nextExecutionKey{}, id)
}

func executionID(ctx context.Context) string {
	id, _ := ctx.Value(executionKey{}).(string)
	return id
}

func beginExecutionContext(ctx context.Context) context.Context {
	id, _ := ctx.Value(nextExecutionKey{}).(string)
	if id == "" {
		id = "exec_" + rand.Text()
	}
	ctx = WithRunExecution(ctx, "")
	return context.WithValue(ctx, executionKey{}, id)
}
