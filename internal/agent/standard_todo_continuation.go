package agent

import (
	"context"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/taskcontract"
)

const maxStandardTodoContinuations = 2

// StandardTodoContinuationPolicy is trusted host metadata for one Run. It is
// intentionally absent from model-visible prompts and schemas: the controller
// decides whether the pristine user turn expects execution, while Agent still
// owns every runtime/evidence exclusion.
type StandardTodoContinuationPolicy struct {
	ExecutionExpected bool
	ShouldYield       func() bool
}

type standardTodoContinuationPolicyKey struct{}

// WithStandardTodoContinuation arms the narrow Standard same-Run continuation
// seam. Direct Agent callers preserve the historical single-final behavior
// unless a trusted host explicitly opts in.
func WithStandardTodoContinuation(ctx context.Context, policy StandardTodoContinuationPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, standardTodoContinuationPolicyKey{}, policy)
}

func standardTodoContinuationPolicyFromContext(ctx context.Context) (StandardTodoContinuationPolicy, bool) {
	if ctx == nil {
		return StandardTodoContinuationPolicy{}, false
	}
	policy, ok := ctx.Value(standardTodoContinuationPolicyKey{}).(StandardTodoContinuationPolicy)
	return policy, ok
}

func (a *Agent) continueStandardTodo(ctx context.Context, state *turnRuntime) bool {
	policy, ok := standardTodoContinuationPolicyFromContext(ctx)
	if !ok || !policy.ExecutionExpected || !a.standardTodoContinuationEligible(state) {
		return false
	}
	if ctx.Err() != nil || policy.ShouldYield != nil && policy.ShouldYield() {
		return false
	}

	progress := a.task.ledger.SuccessfulProgressFingerprint()
	if state.standardTodoContinuations > 0 && progress == state.standardTodoProgress {
		return false
	}
	state.standardTodoContinuations++
	state.standardTodoProgress = progress
	a.sess.conversation.Add(provider.Message{
		Role:    provider.RoleUser,
		Content: a.withTurnPreferences(standardTodoContinuationMessage()),
	})
	return true
}

func (a *Agent) standardTodoContinuationEligible(state *turnRuntime) bool {
	if a == nil || state == nil || a.task.ledger == nil || state.standardTodoContinuations >= maxStandardTodoContinuations {
		return false
	}
	if a.subagentDepth > 0 || a.readOnlyExecution || a.planMode.Load() || a.turn.constraints.ForbidMutation || a.turn.constraints.PlanModeReadOnly {
		return false
	}
	if a.turn.constraints.PolicyFloor == taskcontract.PolicyFloorDelivery || a.closedLoopActive() || a.planContractSnapshot() != nil {
		return false
	}
	if state.graceRound || state.recoveryGraceRound || a.loopGuardAllowsFinal() || !registryHasWriterTools(a.svc.tools) {
		return false
	}
	todos, ok := a.task.ledger.LatestTodos()
	if !ok || len(evidence.IncompleteTodos(todos)) == 0 {
		return false
	}
	active := 0
	for _, todo := range todos {
		if strings.TrimSpace(todo.Status) == "in_progress" {
			active++
		}
	}
	return active == 1
}

func standardTodoContinuationMessage() string {
	return StandardTodoContinuationPrefix + " Continue that item now using available tools. Do not create another plan or end with another promise to act. If blocked, use the appropriate user-input or approval mechanism. Do not repeat external or destructive actions, and do not expand scope."
}
