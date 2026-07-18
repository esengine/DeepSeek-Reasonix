package daemon

import (
	"context"
	"errors"

	"reasonix/internal/remote/host"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeservice"
)

// sessionLifecycleHandlerSet is merged by newTransport. Keeping this family in
// a separate file makes the frozen identity/profile/Goal surface auditable and
// avoids coupling Host business logic to wire construction.
func sessionLifecycleHandlerSet(t *transport) protocol.HandlerSet {
	return protocol.HandlerSet{
		protocol.MethodSessionNew:        t.handleSessionNew,
		protocol.MethodSessionClear:      t.handleSessionClear,
		protocol.MethodSessionFork:       t.handleSessionFork,
		protocol.MethodSessionRewind:     t.handleSessionRewind,
		protocol.MethodSessionProfileSet: t.handleSessionProfileSet,
		protocol.MethodSessionGoalSet:    t.handleSessionGoalSet,
		protocol.MethodSessionGoalResume: t.handleSessionGoalResume,
		protocol.MethodSessionGoalClear:  t.handleSessionGoalClear,
	}
}

// handleDelegatedComposerMutation is the server.go integration point for
// ComposerSubmitMutation's pre-registration ComposerDelegationError. The
// caller passes the original params unchanged; Host business then registers
// MethodSessionSubmit and returns SessionSubmitResult, never a derived typed
// lifecycle claim.
func (t *transport) handleDelegatedComposerMutation(
	ctx context.Context,
	params protocol.SessionSubmitParams,
	delegation *host.ComposerDelegationError,
) (any, error) {
	if delegation == nil {
		return nil, errors.New("remote composer delegation is nil")
	}
	request := lifecycleWireRequest(protocol.MethodSessionSubmit, params.RequestID, params.Target, params)
	var replay protocol.SessionSubmitResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	if delegation.Route.Kind == runtimeservice.ComposerLifecycle && delegation.Route.Lifecycle == runtimeservice.ComposerLifecycleRewind {
		return t.lifecycleService().DelegatedComposerMutation(ctx, params, delegation.Route, t.sessionMutationGuard())
	}
	t.server.catalogMutationMu.Lock()
	defer t.server.catalogMutationMu.Unlock()
	before := t.server.catalog.Revision()
	result, err := t.lifecycleService().DelegatedComposerMutation(ctx, params, delegation.Route, t.sessionMutationGuard())
	changed := t.server.catalog.Revision() != before
	kinds := []protocol.CatalogKind{protocol.CatalogSessions}
	if delegation.Route.Completion == runtimeservice.ComposerCompletionProfileEffort {
		kinds = append(kinds, protocol.CatalogSessionCatalog)
	}
	if err != nil {
		return nil, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, kinds...)
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, kinds...), nil
}

func (t *transport) lifecycleService() host.SessionLifecycleService {
	return host.SessionLifecycleService{Runtimes: t.server.runtimes, Catalog: t.server.catalog, Requests: t.server.requests}
}

func (t *transport) handleSessionNew(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionNewParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := lifecycleWireRequest(protocol.MethodSessionNew, params.RequestID, params.Target, params)
	var replay protocol.SessionNewResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	if _, ok := t.server.runtimes.Runtime(params.Target); !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	t.server.catalogMutationMu.Lock()
	defer t.server.catalogMutationMu.Unlock()
	before := t.server.catalog.Revision()
	result, err := t.lifecycleService().New(ctx, params, t.sessionMutationGuard())
	changed := t.server.catalog.Revision() != before
	if err != nil {
		return nil, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions)
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions), nil
}

func (t *transport) handleSessionClear(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionClearParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := lifecycleWireRequest(protocol.MethodSessionClear, params.RequestID, params.Target, params)
	var replay protocol.SessionClearResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	if _, ok := t.server.runtimes.Runtime(params.Target); !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	t.server.catalogMutationMu.Lock()
	defer t.server.catalogMutationMu.Unlock()
	before := t.server.catalog.Revision()
	result, err := t.lifecycleService().Clear(ctx, params, t.sessionMutationGuard())
	changed := t.server.catalog.Revision() != before
	if err != nil {
		return nil, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions)
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions), nil
}

func (t *transport) handleSessionFork(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionForkParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := lifecycleWireRequest(protocol.MethodSessionFork, params.RequestID, params.Target, params)
	var replay protocol.SessionForkResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	if _, ok := t.server.runtimes.Runtime(params.Target); !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	t.server.catalogMutationMu.Lock()
	defer t.server.catalogMutationMu.Unlock()
	before := t.server.catalog.Revision()
	result, err := t.lifecycleService().Fork(ctx, params, t.sessionMutationGuard())
	changed := t.server.catalog.Revision() != before
	if err != nil {
		return nil, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions)
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions), nil
}

func (t *transport) handleSessionProfileSet(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionProfileSetParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := lifecycleWireRequest(protocol.MethodSessionProfileSet, params.RequestID, params.Target, params)
	var replay protocol.SessionProfileSetResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	if _, ok := t.server.runtimes.Runtime(params.Target); !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	t.server.catalogMutationMu.Lock()
	defer t.server.catalogMutationMu.Unlock()
	before := t.server.catalog.Revision()
	result, err := t.lifecycleService().SetProfile(ctx, params, t.sessionMutationGuard())
	changed := t.server.catalog.Revision() != before
	if err != nil {
		return nil, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogSessionCatalog)
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogSessionCatalog), nil
}

func (t *transport) handleSessionRewind(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionRewindParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := lifecycleWireRequest(protocol.MethodSessionRewind, params.RequestID, params.Target, params)
	var replay protocol.SessionRewindResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.RewindMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handleSessionGoalSet(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionGoalSetParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := lifecycleWireRequest(protocol.MethodSessionGoalSet, params.RequestID, params.Target, params)
	var replay protocol.SessionGoalSetResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.GoalSetMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handleSessionGoalResume(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionGoalResumeParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := lifecycleWireRequest(protocol.MethodSessionGoalResume, params.RequestID, params.Target, params)
	var replay protocol.SessionGoalResumeResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.GoalResumeMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handleSessionGoalClear(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionGoalClearParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := lifecycleWireRequest(protocol.MethodSessionGoalClear, params.RequestID, params.Target, params)
	var replay protocol.SessionGoalClearResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.GoalClearMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func lifecycleWireRequest(method protocol.Method, requestID protocol.RequestID, target protocol.RuntimeTarget, params any) idempotency.Request {
	return idempotency.Request{RequestID: requestID, Method: string(method), Target: idempotency.SessionTarget(target), Params: params}
}

// projectHostSessionMetadata overlays live Goal state captured by the actor.
// projectSnapshot must call this immediately after its catalog metadata read;
// doing so prevents a later sidecar sample from racing Goal mutation.
func projectHostSessionMetadata(snapshot host.RuntimeSnapshot, metadata protocol.SessionMetaSnapshot) protocol.SessionMetaSnapshot {
	if snapshot.Goal == nil {
		metadata.Goal = nil
		metadata.GoalStatus = ""
		return metadata
	}
	goal := *snapshot.Goal
	metadata.Goal = &goal
	metadata.GoalStatus = snapshot.GoalStatus
	return metadata
}
