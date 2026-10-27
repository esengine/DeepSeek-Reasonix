package agent

import (
	"context"
	"fmt"
	"sync/atomic"

	"reasonix/internal/contract/provider"
)

type rootTokenBudget struct {
	used  *atomic.Uint64
	limit uint64
	wall  rootWallBudget
}

type rootTokenBudgetKey struct{}

// InheritRootTokenBudget carries the opaque Agent-root budget scope into a job.
// Token and wall bounds share its lifetime; job cancellation remains separate.
func InheritRootTokenBudget(ctx, parent context.Context) context.Context {
	if scope, ok := parent.Value(rootTokenBudgetKey{}).(*rootTokenBudget); ok {
		ctx = context.WithValue(ctx, rootTokenBudgetKey{}, scope)
		return provider.WithAuxiliaryUsageObserver(ctx, scope.observe)
	}
	return ctx
}

// Auxiliary requests share the active context's scope. Using that scope rather
// than Agent task state also keeps late usage attached to its original task.
func observeRootTokenUsage(ctx context.Context, usage *provider.Usage) {
	scope, _ := ctx.Value(rootTokenBudgetKey{}).(*rootTokenBudget)
	scope.observe(usage)
}

func (a *Agent) bindRootTokenBudget(ctx context.Context, state *turnRuntime) context.Context {
	scope, inherited := ctx.Value(rootTokenBudgetKey{}).(*rootTokenBudget)
	if !inherited {
		previous := a.task.budget.rootTokens
		if previous == nil {
			previous = &rootTokenBudget{used: new(atomic.Uint64)}
			previous.observe(&provider.Usage{PromptTokens: a.task.budget.promptTokens, CompletionTokens: a.task.budget.outputTokens})
		}
		started := a.task.budget.started
		if started.IsZero() {
			started = state.budget.started
		}
		scope = rootBudgetSnapshot(previous, a.taskBudgetLimit(ctx), started)
		a.task.budget.rootTokens = scope
		a.task.budget.rootOrigin = rootBudgetOrigin{session: a.Session(), scopeID: a.task.scopeID}
		ctx = context.WithValue(ctx, rootTokenBudgetKey{}, scope)
	} else if prepared, ok := ctx.Value(preparedRootBudgetKey{}).(preparedRootBudget); ok && prepared.owner == a {
		a.task.budget.rootTokens, a.task.budget.rootOrigin = scope, prepared.origin
	}
	state.budget.rootTokens = scope
	return provider.WithAuxiliaryUsageObserver(ctx, scope.observe)
}

func (b *rootTokenBudget) observe(usage *provider.Usage) {
	if b == nil || usage == nil {
		return
	}
	delta := uint64(max(0, usage.PromptTokens)) + uint64(max(0, usage.CompletionTokens))
	for {
		old := b.used.Load()
		next := old + delta
		if next < old {
			next = ^uint64(0)
		}
		if b.used.CompareAndSwap(old, next) {
			return
		}
	}
}

func (a *Agent) exceededTaskBudget(ctx context.Context, state *turnRuntime) (string, string) {
	if axis, detail := a.task.budget.exceeded(a.taskBudgetLimit(ctx)); axis != "" {
		return axis, detail
	}
	if root := state.budget.rootTokens; root != nil && root.limit > 0 && root.used.Load() >= root.limit {
		return "token", fmt.Sprintf("root task reached its %d token budget across delegated runs", root.limit)
	}
	if root := state.budget.rootTokens; root != nil {
		return root.wall.exceeded()
	}
	return "", ""
}
