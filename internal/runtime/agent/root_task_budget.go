package agent

import (
	"context"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

type rootBudgetOrigin struct {
	session *sessionstore.Session
	scopeID string
}

type preparedRootBudget struct {
	owner  *Agent
	origin rootBudgetOrigin
}

type preparedRootBudgetKey struct{}

// PrepareRootTaskBudget binds task accounting before a host runs companion roles.
// Calls must be serialized with Run, SetSession and ResetTaskBudget. Cancellation
// and local telemetry remain owned by each role; inherited roots stay inherited.
func (a *Agent) PrepareRootTaskBudget(ctx context.Context) context.Context {
	if _, inherited := ctx.Value(rootTokenBudgetKey{}).(*rootTokenBudget); inherited {
		return ctx
	}
	identity, scoped := DeliveryExecutionScopeFromContext(ctx)
	origin := rootBudgetOrigin{session: a.Session(), scopeID: identity.ID}
	previous := a.task.budget.rootTokens
	keep := previous != nil && a.task.budget.rootOrigin.session == origin.session &&
		(a.pending.preserveEvidence || scoped && a.task.budget.rootOrigin.scopeID == identity.ID)
	if !keep {
		previous = &rootTokenBudget{used: new(atomic.Uint64)}
	}
	scope := rootBudgetSnapshot(previous, a.taskBudgetLimit(ctx), time.Now())
	a.task.budget.rootTokens, a.task.budget.rootOrigin = scope, origin
	ctx = context.WithValue(ctx, rootTokenBudgetKey{}, scope)
	ctx = context.WithValue(ctx, preparedRootBudgetKey{}, preparedRootBudget{owner: a, origin: origin})
	return provider.WithAuxiliaryUsageObserver(ctx, scope.observe)
}

func rootBudgetSnapshot(previous *rootTokenBudget, limit TaskBudget, started time.Time) *rootTokenBudget {
	wall := previous.wall
	if wall.started.IsZero() {
		wall.started = started
	}
	wall.limit = limit.Wall
	return &rootTokenBudget{used: previous.used, limit: uint64(max(0, limit.Tokens)), wall: wall}
}
