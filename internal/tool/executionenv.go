package tool

import (
	"context"
	"encoding/json"

	"reasonix/internal/event"
)

// Asker lets a tool put a question to the user. nil in headless runs, where no
// user is there to answer.
type Asker interface {
	Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error)
}

// CallIdentity names the tool call being executed, so a tool that spawns
// further work can nest it under the right card.
type CallIdentity struct {
	ID string
}

// ExecutionEnv is the host capability set one tool call runs against. A call
// site that omits a field here does not compile; a missing context value
// instead reaches the tool as a zero that reads like a legitimate default —
// no asker, not in plan mode, no parent to nest under. It lives at this layer
// because the tools that need it must not import the agent.
type ExecutionEnv struct {
	Call     CallIdentity
	Sink     event.Sink
	Asker    Asker
	PlanMode bool
}

// EnvTool is a Tool that takes its environment explicitly. The registry prefers
// this method when a tool implements it; every other tool keeps receiving the
// same values through the context, so tools migrate one at a time instead of
// on a flag day.
type EnvTool interface {
	Tool
	ExecuteEnv(ctx context.Context, env ExecutionEnv, args json.RawMessage) (string, error)
}

// Execute runs t with env, preferring the explicit path. install adapts env
// onto a context for tools that have not migrated; it is supplied by the agent
// because the legacy context keys are private to it.
func Execute(ctx context.Context, t Tool, env ExecutionEnv, args json.RawMessage, install func(context.Context, ExecutionEnv) context.Context) (string, error) {
	if et, ok := t.(EnvTool); ok {
		return et.ExecuteEnv(ctx, env, args)
	}
	if install != nil {
		ctx = install(ctx, env)
	}
	return t.Execute(ctx, args)
}
