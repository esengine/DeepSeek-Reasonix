package host

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

// RewindMutation resolves the runtime-bound opaque checkpoint and invokes the
// typed Controller rewind transaction.  A partial write is cached as the first
// requestId outcome with exact conservative refresh flags.
func (r *SessionRuntime) RewindMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionRewindParams,
	beforeBegin func() error,
) (protocol.SessionRewindResult, error) {
	request := lifecycleRequest(protocol.MethodSessionRewind, params.RequestID, params.Target, params)
	return runTypedActorMutation(ctx, r, registry, request, params.SessionMutation, beforeBegin, func(state *runtimeActorState, claim *idempotency.Claim) (protocol.SessionRewindResult, error) {
		if err := rejectConflictingSessionWork(r, state, claim, false); err != nil {
			return protocol.SessionRewindResult{}, err
		}
		turn, err := r.resolveCheckpointTurnForState(state, params.CheckpointID, params.Scope != protocol.RewindCode)
		if err != nil {
			return protocol.SessionRewindResult{}, rejectMutation(claim, checkpointLifecycleError(r.target, params.CheckpointID, err))
		}
		scope, err := controlRewindScope(params.Scope)
		if err != nil {
			return protocol.SessionRewindResult{}, rejectMutation(claim, runtimeRemoteError(protocol.ErrCheckpointScopeUnavailable, r.target, "", string(params.CheckpointID)))
		}
		result, err := r.performRewindForState(state, params.CheckpointID, turn, scope)
		if err == nil {
			return result, nil
		}
		var remote *protocol.RemoteError
		if errors.As(err, &remote) {
			return protocol.SessionRewindResult{}, rejectMutation(claim, err)
		}
		return protocol.SessionRewindResult{}, abortMutation(claim, err)
	})
}

func (r *SessionRuntime) performRewindForState(
	state *runtimeActorState,
	checkpointID protocol.CheckpointID,
	turn int,
	scope control.RewindScope,
) (protocol.SessionRewindResult, error) {
	var result control.RewindResult
	var rewindErr error
	if callErr := safeControllerCall(func() { result, rewindErr = r.controller.RewindDetailed(turn, scope) }); callErr != nil {
		return protocol.SessionRewindResult{}, callErr
	}
	// Reconcile even on failure. A conversation rewrite truncates old
	// checkpoints before persistence and those opaque IDs must be stale for
	// the very next actor action, including a retry with another requestId.
	reconcileErr := r.reconcileCheckpointsAfterRewrite(state)
	if rewindErr != nil {
		mapped := mapDetailedRewindError(r.target, checkpointID, rewindErr)
		if reconcileErr != nil {
			mapped = errors.Join(mapped, reconcileErr)
		}
		return protocol.SessionRewindResult{}, mapped
	}
	if reconcileErr != nil {
		return protocol.SessionRewindResult{}, reconcileErr
	}
	return protocol.SessionRewindResult{
		WorkspaceChanged: result.WorkspaceChanged, ConversationRewritten: result.ConversationRewritten,
		SnapshotRequired: true,
	}, nil
}

func (r *SessionRuntime) GoalSetMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionGoalSetParams,
	beforeBegin func() error,
) (protocol.SessionGoalSetResult, error) {
	goal := strings.TrimSpace(params.Goal)
	request := lifecycleRequest(protocol.MethodSessionGoalSet, params.RequestID, params.Target, params)
	return runTypedActorMutation(ctx, r, registry, request, params.SessionMutation, beforeBegin, func(_ *runtimeActorState, claim *idempotency.Claim) (protocol.SessionGoalSetResult, error) {
		if goal == "" {
			return protocol.SessionGoalSetResult{}, rejectMutation(claim, runtimeRemoteError(protocol.ErrQueryFailed, r.target, "", "goal"))
		}
		if err := safeControllerCall(func() { r.controller.SetGoal(goal) }); err != nil {
			return protocol.SessionGoalSetResult{}, abortMutation(claim, err)
		}
		actual, status, err := r.readGoal()
		if err != nil {
			return protocol.SessionGoalSetResult{}, abortMutation(claim, err)
		}
		return protocol.SessionGoalSetResult{Goal: actual, Status: status}, nil
	})
}

func (r *SessionRuntime) GoalResumeMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionGoalResumeParams,
	beforeBegin func() error,
) (protocol.SessionGoalResumeResult, error) {
	request := lifecycleRequest(protocol.MethodSessionGoalResume, params.RequestID, params.Target, params)
	return runTypedActorMutation(ctx, r, registry, request, params.SessionMutation, beforeBegin, func(_ *runtimeActorState, claim *idempotency.Claim) (protocol.SessionGoalResumeResult, error) {
		var resumed bool
		if err := safeControllerCall(func() { resumed = r.controller.ResumeGoal() }); err != nil {
			return protocol.SessionGoalResumeResult{}, abortMutation(claim, err)
		}
		goal, status, err := r.readGoal()
		if err != nil {
			return protocol.SessionGoalResumeResult{}, abortMutation(claim, err)
		}
		return protocol.SessionGoalResumeResult{Resumed: resumed, Goal: goal, Status: status}, nil
	})
}

func (r *SessionRuntime) GoalClearMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionGoalClearParams,
	beforeBegin func() error,
) (protocol.SessionGoalClearResult, error) {
	request := lifecycleRequest(protocol.MethodSessionGoalClear, params.RequestID, params.Target, params)
	return runTypedActorMutation(ctx, r, registry, request, params.SessionMutation, beforeBegin, func(_ *runtimeActorState, claim *idempotency.Claim) (protocol.SessionGoalClearResult, error) {
		if err := safeControllerCall(r.controller.ClearGoal); err != nil {
			return protocol.SessionGoalClearResult{}, abortMutation(claim, err)
		}
		return protocol.SessionGoalClearResult{Cleared: true}, nil
	})
}

type actorMutationBody[T any] func(*runtimeActorState, *idempotency.Claim) (T, error)

func runTypedActorMutation[T any](
	ctx context.Context,
	r *SessionRuntime,
	registry *idempotency.Registry,
	request idempotency.Request,
	mutation protocol.SessionMutation,
	beforeBegin func() error,
	body actorMutationBody[T],
) (T, error) {
	var zero T
	if registry == nil {
		return zero, errors.New("remote idempotency registry is required")
	}
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if beforeBegin != nil {
			if err := beforeBegin(); err != nil {
				return nil, err
			}
		}
		if err := r.preadmitSessionMutation(mutation); err != nil {
			return nil, err
		}
		attempt, err := registry.Begin(request)
		if err != nil {
			return nil, err
		}
		claim, owns := attempt.Claim()
		if !owns {
			return mutationReplay{attempt: attempt}, nil
		}
		result, err := body(state, claim)
		if err != nil {
			return nil, err
		}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		if err := claim.Resolve(outcome); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return zero, err
	}
	if replay, ok := value.(mutationReplay); ok {
		outcome, err := replay.attempt.Wait(ctx)
		if err != nil {
			return zero, err
		}
		var result T
		if err := outcome.Decode(&result); err != nil {
			return zero, err
		}
		return result, nil
	}
	result, ok := value.(T)
	if !ok {
		return zero, errors.New("remote actor mutation returned an invalid result")
	}
	return result, nil
}

func rejectConflictingSessionWork(r *SessionRuntime, state *runtimeActorState, claim *idempotency.Claim, allowPrompt bool) error {
	r.syncActiveJobsForState(state)
	if state.running || state.currentTurn != "" || state.currentOperation != nil || state.activeJobs > 0 || (!allowPrompt && state.pendingPrompt != nil) {
		target := r.target
		return rejectMutation(claim, protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: &target}))
	}
	return nil
}

func controlRewindScope(scope protocol.RewindScope) (control.RewindScope, error) {
	switch scope {
	case protocol.RewindCode:
		return control.RewindCode, nil
	case protocol.RewindConversation:
		return control.RewindConversation, nil
	case protocol.RewindBoth:
		return control.RewindBoth, nil
	default:
		return 0, fmt.Errorf("invalid Remote rewind scope %q", scope)
	}
}

func (r *SessionRuntime) resolveCheckpointIDForTurnForState(
	state *runtimeActorState,
	turn int,
	requireConversation bool,
) (protocol.CheckpointID, error) {
	var snapshot control.CheckpointSnapshot
	if err := safeControllerCall(func() { snapshot = r.controller.CheckpointSnapshot() }); err != nil {
		return "", err
	}
	reconciled, err := r.reconcileCheckpointIDs(state, snapshot.Metas)
	if err != nil {
		return "", err
	}
	state.checkpointIDs = reconciled
	id := reconciled[turn]
	if id == "" {
		return "", ErrCheckpointNotFound
	}
	if requireConversation && !snapshot.ConversationAvailable[turn] {
		return id, ErrCheckpointScopeUnavailable
	}
	return id, nil
}

func mapDetailedRewindError(target protocol.RuntimeTarget, checkpoint protocol.CheckpointID, err error) error {
	var detailed *control.RewindError
	if errors.As(err, &detailed) {
		switch detailed.Failure {
		case control.RewindFailurePartial:
			workspace := detailed.WorkspaceMayHaveChanged
			conversation := detailed.ConversationMayHaveChanged
			snapshot := true
			return protocol.MustRemoteError(protocol.ErrRewindPartial, protocol.ErrorOptions{
				Target: &target, WorkspaceMayHaveChanged: &workspace,
				ConversationMayHaveChanged: &conversation, SnapshotRequired: &snapshot,
			})
		case control.RewindFailureCheckpointMissing:
			return runtimeRemoteError(protocol.ErrCheckpointNotFound, target, "", string(checkpoint))
		case control.RewindFailureScopeUnavailable, control.RewindFailureUnavailable, control.RewindFailureInvalidScope:
			return runtimeRemoteError(protocol.ErrCheckpointScopeUnavailable, target, "", string(checkpoint))
		case control.RewindFailureBusy:
			return runtimeRemoteError(protocol.ErrSessionBusy, target, "", "")
		}
	}
	return err
}

func (r *SessionRuntime) reconcileCheckpointsAfterRewrite(state *runtimeActorState) error {
	var snapshot control.CheckpointSnapshot
	if err := safeControllerCall(func() { snapshot = r.controller.CheckpointSnapshot() }); err != nil {
		return err
	}
	current, err := r.reconcileCheckpointIDs(state, snapshot.Metas)
	if err != nil {
		return err
	}
	state.checkpointIDs = current
	return nil
}

func (r *SessionRuntime) readGoal() (goal string, status protocol.GoalStatus, err error) {
	err = safeControllerCall(func() {
		goal = strings.TrimSpace(r.controller.Goal())
		status = mapGoalStatus(r.controller.GoalStatus())
	})
	return goal, status, err
}
