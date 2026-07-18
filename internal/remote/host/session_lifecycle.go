package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeservice"
	"reasonix/internal/store"
)

// SessionLifecycleService is the Host business boundary for the frozen
// identity-changing Session operations.  It owns RuntimeManager coordination;
// callers own the daemon-wide catalog mutation sequencer and the transport
// lease recheck supplied to each method.
type SessionLifecycleService struct {
	Runtimes *RuntimeManager
	Catalog  *catalog.Catalog
	Requests *idempotency.Registry
}

type lifecycleAdmission struct {
	claim    *idempotency.Claim
	prepared any
	state    *runtimeActorState
	replay   *mutationReplay
	err      error
}

type lifecycleDecision struct {
	result            any
	err               error
	cacheError        bool
	replacementTarget protocol.RuntimeTarget
	replacementEpoch  protocol.RuntimeEpoch
	replacementReason protocol.ResyncReason
	deferResolve      bool
}

type lifecycleBarrier struct {
	runtime  *SessionRuntime
	ready    chan lifecycleAdmission
	decision chan lifecycleDecision
	done     chan lifecycleFinish
}

type lifecycleFinish struct {
	replacement runtimeReplacementResult
	err         error
}

type lifecyclePrepare func(*runtimeActorState) (any, error)

type lifecyclePreparedFork struct {
	turn         int
	path         string
	checkpointID protocol.CheckpointID
}

type lifecycleForkPrepare func(*SessionRuntime, *runtimeActorState) (lifecyclePreparedFork, error)

type lifecycleAbortError struct{ error }

func (s SessionLifecycleService) validate() error {
	if s.Runtimes == nil || s.Catalog == nil || s.Requests == nil {
		return errors.New("remote Session lifecycle service is incomplete")
	}
	return nil
}

// New snapshots the source, creates an empty sibling with the exact persisted
// directories/profile, and migrates the source subscription to the new target.
// The source durable Session remains available for a later cold subscribe.
func (s SessionLifecycleService) New(
	ctx context.Context,
	params protocol.SessionNewParams,
	beforeBegin func() error,
) (protocol.SessionNewResult, error) {
	request := lifecycleRequest(protocol.MethodSessionNew, params.RequestID, params.Target, params)
	return executeSessionNew(ctx, s, params, request, beforeBegin, func(result protocol.SessionNewResult) protocol.SessionNewResult {
		return result
	})
}

func executeSessionNew[T any](
	ctx context.Context,
	s SessionLifecycleService,
	params protocol.SessionNewParams,
	request idempotency.Request,
	beforeBegin func() error,
	project func(protocol.SessionNewResult) T,
) (T, error) {
	var zero T
	if err := s.validate(); err != nil {
		return zero, err
	}
	if replay, found, err := lookupLifecycleOutcome[T](ctx, s.Requests, request); found || err != nil {
		return replay, err
	}
	m := s.Runtimes
	m.mu.Lock()
	runtime, barrier, admission, err := s.beginLocked(request, params.SessionMutation, false, beforeBegin, func(_ *runtimeActorState) (any, error) {
		if err := safeControllerErrorCall(runtimeControllerForPrepare(m, params.Target).Snapshot); err != nil {
			target := params.Target
			return nil, lifecycleAbortError{protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &target})}
		}
		return nil, nil
	})
	if err != nil || admission.replay != nil {
		m.mu.Unlock()
		return waitLifecycleReplay[T](ctx, admission, err)
	}

	created, createErr := s.Catalog.CreateSiblingSession(ctx, params.ExpectedHostEpoch, params.Target)
	if createErr != nil {
		mapped := lifecycleCatalogError(createErr, &params.Target)
		_ = barrier.finish(lifecycleDecision{err: mapped, cacheError: isDeterministicLifecycleError(mapped)})
		m.mu.Unlock()
		return zero, mapped
	}
	replacement, buildErr := m.buildRuntimeLocked(created.Target)
	if buildErr != nil {
		target := created.Target
		rejection := protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{Target: &target})
		_ = barrier.finish(lifecycleDecision{err: rejection, cacheError: true})
		m.mu.Unlock()
		return zero, rejection
	}
	businessResult := protocol.SessionNewResult{
		SourceTarget: params.Target, Target: created.Target, RuntimeEpoch: replacement.epoch,
		Disposition: "created", SnapshotRequired: true,
	}
	result := project(businessResult)
	finish := barrier.finish(lifecycleDecision{
		result: result, replacementTarget: created.Target, replacementEpoch: replacement.epoch,
		replacementReason: protocol.ResyncTargetReplaced,
	})
	if finish.err != nil {
		replacement.discardBuiltRuntime()
		rollbackErr := s.Catalog.RollbackCreatedSession(created.Target)
		m.mu.Unlock()
		return zero, errors.Join(finish.err, rollbackErr)
	}
	m.installReplacementLocked(runtime, replacement, created.Target, finish.replacement)
	m.mu.Unlock()
	<-runtime.Done()
	return result, nil
}

// Clear retires the old durable identity and installs a fresh empty sibling.
// Physical cleanup occurs only after the old Controller is stopped.
func (s SessionLifecycleService) Clear(
	ctx context.Context,
	params protocol.SessionClearParams,
	beforeBegin func() error,
) (protocol.SessionClearResult, error) {
	request := lifecycleRequest(protocol.MethodSessionClear, params.RequestID, params.Target, params)
	return executeSessionClear(ctx, s, params, request, beforeBegin, func(result protocol.SessionClearResult) protocol.SessionClearResult {
		return result
	})
}

func executeSessionClear[T any](
	ctx context.Context,
	s SessionLifecycleService,
	params protocol.SessionClearParams,
	request idempotency.Request,
	beforeBegin func() error,
	project func(protocol.SessionClearResult) T,
) (T, error) {
	var zero T
	if err := s.validate(); err != nil {
		return zero, err
	}
	if replay, found, err := lookupLifecycleOutcome[T](ctx, s.Requests, request); found || err != nil {
		return replay, err
	}
	m := s.Runtimes
	m.mu.Lock()
	runtime, barrier, admission, err := s.beginLocked(request, params.SessionMutation, false, beforeBegin, nil)
	if err != nil || admission.replay != nil {
		m.mu.Unlock()
		return waitLifecycleReplay[T](ctx, admission, err)
	}
	transition, transitionErr := s.Catalog.BeginClear(ctx, params.ExpectedHostEpoch, params.Target)
	if transitionErr != nil {
		mapped := lifecycleCatalogError(transitionErr, &params.Target)
		_ = barrier.finish(lifecycleDecision{err: mapped, cacheError: isDeterministicLifecycleError(mapped)})
		m.mu.Unlock()
		return zero, mapped
	}
	replacement, buildErr := m.buildRuntimeLocked(transition.Replacement.Target)
	if buildErr != nil {
		rollbackErr := transition.Rollback()
		target := params.Target
		rejection := protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{Target: &target})
		_ = barrier.finish(lifecycleDecision{err: rejection, cacheError: true})
		m.mu.Unlock()
		return zero, errors.Join(rejection, rollbackErr)
	}
	businessResult := protocol.SessionClearResult{
		PreviousTarget: params.Target, Target: transition.Replacement.Target,
		RuntimeEpoch: replacement.epoch, Disposition: protocol.SessionCleared, SnapshotRequired: true,
	}
	result := project(businessResult)
	finish := barrier.finish(lifecycleDecision{
		result: result, replacementTarget: transition.Replacement.Target, replacementEpoch: replacement.epoch,
		replacementReason: protocol.ResyncTargetReplaced, deferResolve: true,
	})
	if finish.err != nil {
		replacement.discardBuiltRuntime()
		rollbackErr := transition.Rollback()
		m.mu.Unlock()
		return zero, errors.Join(finish.err, rollbackErr)
	}
	m.installReplacementLocked(runtime, replacement, transition.Replacement.Target, finish.replacement)
	m.mu.Unlock()
	<-runtime.Done()
	disposition, cleanupErr := transition.CleanupPrevious()
	businessResult.Disposition = disposition
	result = project(businessResult)
	if resolveErr := resolveLifecycleClaim(admission.claim, result); resolveErr != nil {
		return zero, errors.Join(resolveErr, cleanupErr)
	}
	// cleanup_pending is a successful logical Clear.  The caller may report the
	// diagnostic internally but replays must return this exact disposition.
	return result, nil
}

// DelegatedComposerMutation executes raw composer routes that need catalog or
// RuntimeManager coordination. It deliberately claims the original
// session/submit request, including the complete SessionSubmitParams, and
// caches SessionSubmitResult. Typed lifecycle methods are only the shared
// business input; their method names and result shapes never enter this claim.
func (s SessionLifecycleService) DelegatedComposerMutation(
	ctx context.Context,
	params protocol.SessionSubmitParams,
	route runtimeservice.ComposerRoute,
	beforeBegin func() error,
) (protocol.SessionSubmitResult, error) {
	request := lifecycleRequest(protocol.MethodSessionSubmit, params.RequestID, params.Target, params)
	if route.Input != params.Input {
		return protocol.SessionSubmitResult{}, errors.New("remote composer delegation route does not match the original input")
	}
	switch {
	case route.Kind == runtimeservice.ComposerLifecycle && route.Lifecycle == runtimeservice.ComposerLifecycleNew:
		lifecycleParams := protocol.SessionNewParams{SessionMutation: params.SessionMutation}
		return executeSessionNew(ctx, s, lifecycleParams, request, beforeBegin, func(result protocol.SessionNewResult) protocol.SessionSubmitResult {
			return protocol.SessionSubmitResult{
				Kind: protocol.SubmitCompleted, Effect: protocol.EffectSessionReplaced,
				Target: result.Target, RuntimeEpoch: result.RuntimeEpoch, SnapshotRequired: true,
			}
		})
	case route.Kind == runtimeservice.ComposerLifecycle && route.Lifecycle == runtimeservice.ComposerLifecycleClear:
		lifecycleParams := protocol.SessionClearParams{SessionMutation: params.SessionMutation}
		return executeSessionClear(ctx, s, lifecycleParams, request, beforeBegin, func(result protocol.SessionClearResult) protocol.SessionSubmitResult {
			return protocol.SessionSubmitResult{
				Kind: protocol.SubmitCompleted, Effect: protocol.EffectSessionReplaced,
				Target: result.Target, RuntimeEpoch: result.RuntimeEpoch, SnapshotRequired: true,
			}
		})
	case route.Kind == runtimeservice.ComposerLifecycle && route.Lifecycle == runtimeservice.ComposerLifecycleBranch:
		turn, name, fromTurn, err := control.ParseBranchTarget(route.Argument)
		if err != nil {
			return protocol.SessionSubmitResult{}, err
		}
		return executeSessionFork(ctx, s, params.SessionMutation, request, beforeBegin, true,
			func(sourceRuntime *SessionRuntime, state *runtimeActorState) (lifecyclePreparedFork, error) {
				var checkpointID protocol.CheckpointID
				var path string
				if fromTurn {
					checkpointID, err = sourceRuntime.resolveCheckpointIDForTurnForState(state, turn-1, true)
					if err != nil {
						return lifecyclePreparedFork{}, checkpointLifecycleError(sourceRuntime.target, checkpointID, err)
					}
					if callErr := safeControllerCall(func() { path, err = sourceRuntime.controller.ForkSession(turn-1, name) }); callErr != nil {
						return lifecyclePreparedFork{}, lifecycleAbortError{callErr}
					}
				} else {
					if callErr := safeControllerCall(func() { path, err = sourceRuntime.controller.BranchSession(name) }); callErr != nil {
						return lifecyclePreparedFork{}, lifecycleAbortError{callErr}
					}
				}
				if err != nil {
					return lifecyclePreparedFork{}, lifecycleAbortError{err}
				}
				return lifecyclePreparedFork{turn: turn - 1, path: path, checkpointID: checkpointID}, nil
			},
			func(result protocol.SessionForkResult) protocol.SessionSubmitResult {
				return protocol.SessionSubmitResult{
					Kind: protocol.SubmitCompleted, Effect: protocol.EffectSessionReplaced,
					Target: result.ChildTarget, RuntimeEpoch: result.ChildRuntimeEpoch, SnapshotRequired: true,
				}
			},
		)
	case route.Kind == runtimeservice.ComposerLifecycle && route.Lifecycle == runtimeservice.ComposerLifecycleRewind:
		return executeComposerRewind(ctx, s, params, request, route.Argument, beforeBegin)
	case route.Kind == runtimeservice.ComposerCompleted && route.Completion == runtimeservice.ComposerCompletionProfileEffort:
		effort := strings.TrimSpace(route.Argument)
		if effort == "" {
			return protocol.SessionSubmitResult{}, protocol.MustRemoteError(protocol.ErrInvalidProfile, protocol.ErrorOptions{Target: &params.Target})
		}
		profileParams := protocol.SessionProfileSetParams{
			SessionMutation: params.SessionMutation,
			Patch:           protocol.ProfilePatch{Effort: &effort},
		}
		return executeSessionProfileSet(ctx, s, profileParams, request, beforeBegin, func(result protocol.SessionProfileSetResult) protocol.SessionSubmitResult {
			return projectComposerProfileResult(params.Target, result)
		})
	default:
		return protocol.SessionSubmitResult{}, &ComposerDelegationError{Route: route}
	}
}

func executeComposerRewind(
	ctx context.Context,
	s SessionLifecycleService,
	params protocol.SessionSubmitParams,
	request idempotency.Request,
	args string,
	beforeBegin func() error,
) (protocol.SessionSubmitResult, error) {
	if err := s.validate(); err != nil {
		return protocol.SessionSubmitResult{}, err
	}
	if replay, found, err := lookupLifecycleOutcome[protocol.SessionSubmitResult](ctx, s.Requests, request); found || err != nil {
		return replay, err
	}
	m := s.Runtimes
	m.mu.Lock()
	sourceRuntime := m.runtimes[params.Target]
	_, barrier, admission, err := s.beginLocked(request, params.SessionMutation, false, beforeBegin, func(state *runtimeActorState) (any, error) {
		if sourceRuntime == nil {
			return nil, runtimeRemoteError(protocol.ErrSessionNotFound, params.Target, "", "")
		}
		var snapshot control.CheckpointSnapshot
		if callErr := safeControllerCall(func() { snapshot = sourceRuntime.controller.CheckpointSnapshot() }); callErr != nil {
			return nil, lifecycleAbortError{callErr}
		}
		turn, scope, parseErr := control.ParseRewindTarget(args, snapshot.Metas)
		if parseErr != nil {
			return nil, runtimeRemoteError(protocol.ErrCheckpointNotFound, params.Target, "", "")
		}
		checkpointID, resolveErr := sourceRuntime.resolveCheckpointIDForTurnForState(state, turn, scope != control.RewindCode)
		if resolveErr != nil {
			return nil, checkpointLifecycleError(params.Target, checkpointID, resolveErr)
		}
		_, rewindErr := sourceRuntime.performRewindForState(state, checkpointID, turn, scope)
		if rewindErr != nil {
			var remote *protocol.RemoteError
			if errors.As(rewindErr, &remote) {
				return nil, rewindErr
			}
			return nil, lifecycleAbortError{rewindErr}
		}
		return protocol.SessionSubmitResult{
			Kind: protocol.SubmitCompleted, Effect: protocol.EffectStateChanged,
			Target: params.Target, RuntimeEpoch: sourceRuntime.epoch, SnapshotRequired: true,
		}, nil
	})
	if err != nil || admission.replay != nil {
		m.mu.Unlock()
		return waitLifecycleReplay[protocol.SessionSubmitResult](ctx, admission, err)
	}
	result := admission.prepared.(protocol.SessionSubmitResult)
	finish := barrier.finish(lifecycleDecision{result: result})
	if finish.err != nil {
		m.mu.Unlock()
		return protocol.SessionSubmitResult{}, finish.err
	}
	m.mu.Unlock()
	return result, nil
}

func projectComposerProfileResult(target protocol.RuntimeTarget, result protocol.SessionProfileSetResult) protocol.SessionSubmitResult {
	effect := protocol.EffectStateChanged
	snapshotRequired := false
	if result.Disposition == protocol.ProfileRebuilt {
		effect = protocol.EffectRuntimeReplaced
		snapshotRequired = true
	}
	return protocol.SessionSubmitResult{
		Kind: protocol.SubmitCompleted, Effect: effect,
		Target: target, RuntimeEpoch: result.RuntimeEpoch, SnapshotRequired: snapshotRequired,
	}
}

// Fork invokes the real Controller fork primitive inside the source actor,
// adopts the resulting transcript into the Remote catalog, and starts an
// independent child runtime without replacing or switching the source.
func (s SessionLifecycleService) Fork(
	ctx context.Context,
	params protocol.SessionForkParams,
	beforeBegin func() error,
) (protocol.SessionForkResult, error) {
	request := lifecycleRequest(protocol.MethodSessionFork, params.RequestID, params.Target, params)
	return executeSessionFork(ctx, s, params.SessionMutation, request, beforeBegin, false,
		func(sourceRuntime *SessionRuntime, state *runtimeActorState) (lifecyclePreparedFork, error) {
			turn, err := sourceRuntime.resolveCheckpointTurnForState(state, params.CheckpointID, true)
			if err != nil {
				return lifecyclePreparedFork{}, checkpointLifecycleError(sourceRuntime.target, params.CheckpointID, err)
			}
			var path string
			if callErr := safeControllerCall(func() { path, err = sourceRuntime.controller.ForkSession(turn, strings.TrimSpace(params.Name)) }); callErr != nil {
				return lifecyclePreparedFork{}, lifecycleAbortError{callErr}
			}
			if err != nil {
				return lifecyclePreparedFork{}, lifecycleAbortError{err}
			}
			return lifecyclePreparedFork{turn: turn, path: path, checkpointID: params.CheckpointID}, nil
		},
		func(result protocol.SessionForkResult) protocol.SessionForkResult { return result },
	)
}

func executeSessionFork[T any](
	ctx context.Context,
	s SessionLifecycleService,
	mutation protocol.SessionMutation,
	request idempotency.Request,
	beforeBegin func() error,
	replaceSource bool,
	prepare lifecycleForkPrepare,
	project func(protocol.SessionForkResult) T,
) (T, error) {
	var zero T
	if err := s.validate(); err != nil {
		return zero, err
	}
	if replay, found, err := lookupLifecycleOutcome[T](ctx, s.Requests, request); found || err != nil {
		return replay, err
	}
	m := s.Runtimes
	m.mu.Lock()
	sourceRuntime := m.runtimes[mutation.Target]
	runtime, barrier, admission, err := s.beginLocked(request, mutation, false, beforeBegin, func(state *runtimeActorState) (any, error) {
		if sourceRuntime == nil {
			return nil, runtimeRemoteError(protocol.ErrSessionNotFound, mutation.Target, "", "")
		}
		prepared, err := prepare(sourceRuntime, state)
		if err != nil {
			return nil, err
		}
		return prepared, nil
	})
	if err != nil || admission.replay != nil {
		m.mu.Unlock()
		return waitLifecycleReplay[T](ctx, admission, err)
	}
	prepared := admission.prepared.(lifecyclePreparedFork)
	var created catalog.LifecycleCreated
	var adoptErr error
	if prepared.checkpointID == "" {
		created, adoptErr = s.Catalog.AdoptBranch(ctx, mutation.ExpectedHostEpoch, mutation.Target, prepared.path)
	} else {
		created, adoptErr = s.Catalog.AdoptFork(ctx, mutation.ExpectedHostEpoch, mutation.Target, prepared.checkpointID, prepared.path)
	}
	if adoptErr != nil {
		_ = removeForkArtifacts(prepared.path)
		mapped := lifecycleCatalogError(adoptErr, &mutation.Target)
		_ = barrier.finish(lifecycleDecision{err: mapped, cacheError: isDeterministicLifecycleError(mapped)})
		m.mu.Unlock()
		return zero, mapped
	}
	child, buildErr := m.buildRuntimeLocked(created.Target)
	if buildErr != nil {
		rollbackErr := s.Catalog.RollbackCreatedSession(created.Target)
		target := created.Target
		rejection := protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{Target: &target})
		_ = barrier.finish(lifecycleDecision{err: rejection, cacheError: true})
		m.mu.Unlock()
		return zero, errors.Join(rejection, rollbackErr)
	}
	businessResult := protocol.SessionForkResult{
		SourceTarget: mutation.Target, SourceRuntimeEpoch: runtime.epoch,
		ChildTarget: created.Target, ChildRuntimeEpoch: child.epoch,
	}
	result := project(businessResult)
	decision := lifecycleDecision{result: result}
	if replaceSource {
		decision.replacementTarget = created.Target
		decision.replacementEpoch = child.epoch
		decision.replacementReason = protocol.ResyncTargetReplaced
	}
	finish := barrier.finish(decision)
	if finish.err != nil {
		child.discardBuiltRuntime()
		rollbackErr := s.Catalog.RollbackCreatedSession(created.Target)
		m.mu.Unlock()
		return zero, errors.Join(finish.err, rollbackErr)
	}
	if m.runtimes[mutation.Target] != runtime || m.runtimes[created.Target] != nil {
		child.discardBuiltRuntime()
		rollbackErr := s.Catalog.RollbackCreatedSession(created.Target)
		m.mu.Unlock()
		return zero, errors.Join(errors.New("remote fork lost runtime ownership"), rollbackErr)
	}
	if replaceSource {
		m.installReplacementLocked(runtime, child, created.Target, finish.replacement)
		m.mu.Unlock()
		<-runtime.Done()
		return result, nil
	}
	m.runtimes[created.Target] = child
	child.start()
	m.mu.Unlock()
	return result, nil
}

// SetProfile performs either an in-place controller update or exactly one
// same-target runtime rebuild.  The complete merged profile is persisted
// first and rolled back if Controller application/build fails.
func (s SessionLifecycleService) SetProfile(
	ctx context.Context,
	params protocol.SessionProfileSetParams,
	beforeBegin func() error,
) (protocol.SessionProfileSetResult, error) {
	request := lifecycleRequest(protocol.MethodSessionProfileSet, params.RequestID, params.Target, params)
	return executeSessionProfileSet(ctx, s, params, request, beforeBegin, func(result protocol.SessionProfileSetResult) protocol.SessionProfileSetResult {
		return result
	})
}

func executeSessionProfileSet[T any](
	ctx context.Context,
	s SessionLifecycleService,
	params protocol.SessionProfileSetParams,
	request idempotency.Request,
	beforeBegin func() error,
	project func(protocol.SessionProfileSetResult) T,
) (T, error) {
	var zero T
	if err := s.validate(); err != nil {
		return zero, err
	}
	rebuild := params.Patch.Model != nil || params.Patch.Effort != nil || params.Patch.TokenMode != nil
	if replay, found, err := lookupLifecycleOutcome[T](ctx, s.Requests, request); found || err != nil {
		return replay, err
	}
	m := s.Runtimes
	m.mu.Lock()
	runtime, barrier, admission, err := s.beginLocked(request, params.SessionMutation, !rebuild, beforeBegin, nil)
	if err != nil || admission.replay != nil {
		m.mu.Unlock()
		return waitLifecycleReplay[T](ctx, admission, err)
	}
	transition, patchErr := s.Catalog.BeginProfilePatch(ctx, params.ExpectedHostEpoch, params.Target, params.Patch)
	if patchErr != nil {
		mapped := lifecycleCatalogError(patchErr, &params.Target)
		_ = barrier.finish(lifecycleDecision{err: mapped, cacheError: isDeterministicLifecycleError(mapped)})
		m.mu.Unlock()
		return zero, mapped
	}
	if rebuild {
		replacement, buildErr := m.buildRuntimeLocked(params.Target)
		if buildErr != nil {
			rollbackErr := transition.Rollback()
			target := params.Target
			rejection := protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{Target: &target})
			_ = barrier.finish(lifecycleDecision{err: rejection, cacheError: true})
			m.mu.Unlock()
			return zero, errors.Join(rejection, rollbackErr)
		}
		businessResult := protocol.SessionProfileSetResult{
			ResolvedProfile: transition.Current, RuntimeEpoch: replacement.epoch,
			Disposition: protocol.ProfileRebuilt, AutoResolvedPromptIDs: []protocol.PromptID{},
		}
		result := project(businessResult)
		finish := barrier.finish(lifecycleDecision{
			result: result, replacementTarget: params.Target, replacementEpoch: replacement.epoch,
			replacementReason: protocol.ResyncRuntimeReplaced,
		})
		if finish.err != nil {
			replacement.discardBuiltRuntime()
			rollbackErr := transition.Rollback()
			m.mu.Unlock()
			return zero, errors.Join(finish.err, rollbackErr)
		}
		transition.Commit()
		m.installReplacementLocked(runtime, replacement, params.Target, finish.replacement)
		m.mu.Unlock()
		<-runtime.Done()
		return result, nil
	}

	autoResolved, applyErr := applyInPlaceProfile(runtime, admission.state, transition.Previous, transition.Current)
	if applyErr != nil {
		rollbackErr := transition.Rollback()
		_ = applyInPlaceProfileRollback(runtime, transition.Previous)
		_ = barrier.finish(lifecycleDecision{err: applyErr})
		m.mu.Unlock()
		return zero, errors.Join(applyErr, rollbackErr)
	}
	businessResult := protocol.SessionProfileSetResult{
		ResolvedProfile: transition.Current, RuntimeEpoch: runtime.epoch,
		Disposition: protocol.ProfileUpdated, AutoResolvedPromptIDs: autoResolved,
	}
	result := project(businessResult)
	finish := barrier.finish(lifecycleDecision{result: result})
	if finish.err != nil {
		rollbackErr := transition.Rollback()
		_ = applyInPlaceProfileRollback(runtime, transition.Previous)
		m.mu.Unlock()
		return zero, errors.Join(finish.err, rollbackErr)
	}
	transition.Commit()
	m.mu.Unlock()
	return result, nil
}

func (s SessionLifecycleService) beginLocked(
	request idempotency.Request,
	mutation protocol.SessionMutation,
	allowPrompt bool,
	beforeBegin func() error,
	prepare lifecyclePrepare,
) (*SessionRuntime, *lifecycleBarrier, lifecycleAdmission, error) {
	m := s.Runtimes
	if m.closed || m.ctx.Err() != nil {
		return nil, nil, lifecycleAdmission{}, ErrRuntimeManagerClosed
	}
	// Close the lookup-to-manager-lock race for identity replacements. A
	// concurrent first request may have retired the source runtime while this
	// request waited for m.mu; its exact outcome must still replay instead of
	// degrading to SESSION_NOT_FOUND.
	if attempt, found, err := s.Requests.Lookup(request); err != nil {
		return nil, nil, lifecycleAdmission{}, err
	} else if found {
		replay := mutationReplay{attempt: attempt}
		return nil, nil, lifecycleAdmission{replay: &replay}, nil
	}
	runtime := m.runtimes[mutation.Target]
	if runtime == nil {
		return nil, nil, lifecycleAdmission{}, runtimeRemoteError(protocol.ErrSessionNotFound, mutation.Target, "", "")
	}
	barrier, admission, err := runtime.beginLifecycleMutation(s.Requests, request, mutation, allowPrompt, beforeBegin, prepare)
	return runtime, barrier, admission, err
}

func (r *SessionRuntime) beginLifecycleMutation(
	registry *idempotency.Registry,
	request idempotency.Request,
	mutation protocol.SessionMutation,
	allowPrompt bool,
	beforeBegin func() error,
	prepare lifecyclePrepare,
) (*lifecycleBarrier, lifecycleAdmission, error) {
	if registry == nil {
		return nil, lifecycleAdmission{}, errors.New("remote idempotency registry is required")
	}
	if !r.accepting.Load() {
		return nil, lifecycleAdmission{}, ErrRuntimeClosed
	}
	barrier := &lifecycleBarrier{
		runtime: r, ready: make(chan lifecycleAdmission, 1),
		decision: make(chan lifecycleDecision, 1), done: make(chan lifecycleFinish, 1),
	}
	if !r.mailbox.enqueueAdmission(func(state *runtimeActorState) {
		fail := func(err error) {
			barrier.ready <- lifecycleAdmission{err: err}
			barrier.done <- lifecycleFinish{err: err}
		}
		if beforeBegin != nil {
			if err := beforeBegin(); err != nil {
				fail(err)
				return
			}
		}
		attempt, err := registry.Begin(request)
		if err != nil {
			fail(err)
			return
		}
		claim, owns := attempt.Claim()
		if !owns {
			replay := mutationReplay{attempt: attempt}
			barrier.ready <- lifecycleAdmission{replay: &replay}
			barrier.done <- lifecycleFinish{}
			return
		}
		if err := r.preadmitSessionMutation(mutation); err != nil {
			fail(abortMutation(claim, err))
			return
		}
		r.syncActiveJobsForState(state)
		busy := state.running || state.currentTurn != "" || state.currentOperation != nil || state.activeJobs > 0 || (!allowPrompt && state.pendingPrompt != nil)
		if busy {
			target := r.target
			fail(rejectMutation(claim, protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: &target})))
			return
		}
		var prepared any
		if prepare != nil {
			prepared, err = prepare(state)
			if err != nil {
				var abort lifecycleAbortError
				if errors.As(err, &abort) {
					fail(abortMutation(claim, abort.error))
					return
				}
				var remote *protocol.RemoteError
				if errors.As(err, &remote) {
					fail(rejectMutation(claim, err))
				} else {
					fail(abortMutation(claim, err))
				}
				return
			}
		}
		barrier.ready <- lifecycleAdmission{claim: claim, prepared: prepared, state: state}
		decision := <-barrier.decision
		if decision.err != nil {
			if decision.cacheError {
				err = rejectMutation(claim, decision.err)
			} else {
				err = abortMutation(claim, decision.err)
			}
			barrier.done <- lifecycleFinish{err: err}
			return
		}
		if !decision.deferResolve {
			if err = resolveLifecycleClaim(claim, decision.result); err != nil {
				barrier.done <- lifecycleFinish{err: err}
				return
			}
		}
		finish := lifecycleFinish{}
		if decision.replacementEpoch != "" {
			finish.replacement = r.retireForLifecycleReplacement(state, decision)
		}
		barrier.done <- finish
	}) {
		return nil, lifecycleAdmission{}, ErrRuntimeClosed
	}
	select {
	case admission := <-barrier.ready:
		if admission.err != nil || admission.replay != nil {
			<-barrier.done
			return nil, admission, admission.err
		}
		return barrier, admission, nil
	case <-r.done:
		return nil, lifecycleAdmission{}, ErrRuntimeClosed
	}
}

func (b *lifecycleBarrier) finish(decision lifecycleDecision) lifecycleFinish {
	if b == nil {
		return lifecycleFinish{err: errors.New("remote lifecycle barrier is nil")}
	}
	b.decision <- decision
	return <-b.done
}

func (r *SessionRuntime) retireForLifecycleReplacement(state *runtimeActorState, decision lifecycleDecision) runtimeReplacementResult {
	r.accepting.Store(false)
	r.current.Store(false)
	result := runtimeReplacementResult{retired: make([]*retiredSubscription, 0, len(state.subscriptions))}
	for id, subscription := range state.subscriptions {
		delete(state.subscriptions, id)
		if !subscription.overflowed {
			subscription.overflowed = true
			drainSubscription(subscription.messages)
			resync := &ResyncRequired{
				SubscriptionID: subscription.id, HostEpoch: r.hostEpoch, Target: r.target,
				RuntimeEpoch: r.epoch, LastSeq: subscription.lastSeq, Reason: decision.replacementReason,
				ReplacementRuntimeEpoch: decision.replacementEpoch,
			}
			if decision.replacementReason == protocol.ResyncTargetReplaced {
				target := decision.replacementTarget
				resync.ReplacementTarget = &target
			}
			subscription.messages <- SubscriptionMessage{Resync: resync}
		}
		result.retired = append(result.retired, &retiredSubscription{
			state: subscription, replacementTarget: decision.replacementTarget,
			replacementRuntimeEpoch: decision.replacementEpoch, migrating: subscription.migrating,
		})
	}
	resetRuntimeLiveState(state)
	state.stopping = true
	return result
}

func (m *RuntimeManager) installReplacementLocked(previous, replacement *SessionRuntime, target protocol.RuntimeTarget, retired runtimeReplacementResult) {
	delete(m.runtimes, previous.target)
	m.runtimes[target] = replacement
	for _, item := range retired.retired {
		m.retiredSubscriptions[item.state.id] = item
	}
	replacement.start()
}

func lifecycleRequest(method protocol.Method, requestID protocol.RequestID, target protocol.RuntimeTarget, params any) idempotency.Request {
	return idempotency.Request{RequestID: requestID, Method: string(method), Target: idempotency.SessionTarget(target), Params: params}
}

func waitLifecycleReplay[T any](ctx context.Context, admission lifecycleAdmission, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	if admission.replay == nil {
		return zero, errors.New("remote lifecycle admission returned no claim")
	}
	outcome, err := admission.replay.attempt.Wait(ctx)
	if err != nil {
		return zero, err
	}
	var result T
	if err := outcome.Decode(&result); err != nil {
		return zero, err
	}
	return result, nil
}

func lookupLifecycleOutcome[T any](ctx context.Context, registry *idempotency.Registry, request idempotency.Request) (T, bool, error) {
	var zero T
	attempt, found, err := registry.Lookup(request)
	if err != nil || !found {
		return zero, found, err
	}
	outcome, err := attempt.Wait(ctx)
	if err != nil {
		return zero, true, err
	}
	var result T
	if err := outcome.Decode(&result); err != nil {
		return zero, true, err
	}
	return result, true, nil
}

func lifecycleCatalogError(err error, target *protocol.RuntimeTarget) error {
	code, ok := catalog.ErrorCode(err)
	if !ok {
		return err
	}
	options := protocol.ErrorOptions{}
	if target != nil {
		copyTarget := *target
		options.Target = &copyTarget
	}
	remote, mapErr := protocol.NewRemoteError(code, options)
	if mapErr != nil {
		return errors.Join(err, mapErr)
	}
	return remote
}

func isDeterministicLifecycleError(err error) bool {
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) || remote == nil {
		return false
	}
	switch remote.Code {
	case protocol.ErrSessionPersistFailed, protocol.ErrRuntimeStartFailed, protocol.ErrQueryFailed:
		return false
	default:
		return true
	}
}

func checkpointLifecycleError(target protocol.RuntimeTarget, checkpoint protocol.CheckpointID, err error) error {
	switch {
	case errors.Is(err, ErrCheckpointNotFound):
		return runtimeRemoteError(protocol.ErrCheckpointNotFound, target, "", string(checkpoint))
	case errors.Is(err, ErrCheckpointScopeUnavailable):
		return runtimeRemoteError(protocol.ErrCheckpointScopeUnavailable, target, "", string(checkpoint))
	default:
		return err
	}
}

func runtimeControllerForPrepare(m *RuntimeManager, target protocol.RuntimeTarget) control.SessionAPI {
	if m == nil || m.runtimes[target] == nil {
		return nil
	}
	return m.runtimes[target].controller
}

type inPlaceProfileController interface {
	ApplyToolApprovalMode(string) []string
}

func applyInPlaceProfile(runtime *SessionRuntime, state *runtimeActorState, previous, current protocol.ResolvedProfile) ([]protocol.PromptID, error) {
	if runtime == nil || runtime.controller == nil {
		return nil, ErrControllerUnavailable
	}
	if state == nil {
		return nil, errors.New("remote profile update lost its actor barrier")
	}
	var controllerPromptIDs []string
	err := safeControllerCall(func() {
		if previous.CollaborationMode != current.CollaborationMode {
			runtime.controller.SetPlanMode(current.CollaborationMode == protocol.CollaborationPlan)
		}
		if previous.ToolApprovalMode != current.ToolApprovalMode {
			if controller, ok := runtime.controller.(inPlaceProfileController); ok {
				controllerPromptIDs = controller.ApplyToolApprovalMode(string(current.ToolApprovalMode))
			} else {
				runtime.controller.SetToolApprovalMode(string(current.ToolApprovalMode))
			}
		}
	})
	if err != nil {
		return nil, err
	}
	resolved := make([]protocol.PromptID, 0, 1)
	if len(controllerPromptIDs) != 0 {
		wanted := make(map[string]struct{}, len(controllerPromptIDs))
		for _, id := range controllerPromptIDs {
			wanted[id] = struct{}{}
		}
		// The actor is held by lifecycleBarrier while this helper runs. Only the
		// current prompt has a public opaque mapping; other Controller queue
		// entries are intentionally not guessed or exposed.
		if state.pendingPrompt != nil {
			if _, ok := wanted[state.pendingPrompt.controllerID]; ok {
				resolved = append(resolved, state.pendingPrompt.id)
				clearPendingPrompt(state)
			}
		}
	}
	return resolved, nil
}

func applyInPlaceProfileRollback(runtime *SessionRuntime, profile protocol.ResolvedProfile) error {
	return safeControllerCall(func() {
		runtime.controller.SetPlanMode(profile.CollaborationMode == protocol.CollaborationPlan)
		runtime.controller.SetToolApprovalMode(string(profile.ToolApprovalMode))
	})
}

func removeForkArtifacts(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	paths := []string{path}
	paths = append(paths, store.SessionSidecarFiles(path)...)
	paths = append(paths,
		store.SessionCheckpointDir(path), store.SessionJobsDir(path), store.SessionCleanupPending(path),
		store.SessionLockFile(path), store.SessionLeaseLock(path), store.SessionLeaseInfo(path),
	)
	var combined error
	for _, artifact := range paths {
		combined = errors.Join(combined, removePath(artifact))
	}
	return combined
}

func removePath(path string) error {
	if err := osRemoveAll(path); err != nil {
		return err
	}
	return nil
}

var osRemoveAll = func(path string) error {
	// Kept as a package variable only to make pre-adoption fork cleanup
	// injectable in narrow Host tests without exporting filesystem policy.
	return removeAllPath(path)
}

func removeAllPath(path string) error {
	return os.RemoveAll(path)
}

func resolveLifecycleClaim(claim *idempotency.Claim, result any) error {
	if claim == nil {
		return errors.New("remote lifecycle idempotency claim is nil")
	}
	outcome, err := idempotency.PrepareSuccess(result)
	if err != nil {
		_ = claim.Abort(err)
		return err
	}
	if err := claim.Resolve(outcome); err != nil {
		_ = claim.Abort(err)
		return err
	}
	return nil
}

func mapGoalStatus(value string) protocol.GoalStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(protocol.GoalComplete):
		return protocol.GoalComplete
	case string(protocol.GoalBlocked):
		return protocol.GoalBlocked
	case string(protocol.GoalStopped):
		return protocol.GoalStopped
	default:
		return protocol.GoalRunning
	}
}

func lifecycleInternalError(label string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("remote %s: %w", label, err)
}
