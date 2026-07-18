package host

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reasonix/internal/autoresearch"
	"reasonix/internal/control"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
)

func (r *SessionRuntime) runtimeSessionRef() runtimeapi.SessionRef {
	return runtimeapi.SessionRef{
		WorkspaceID: runtimeapi.WorkspaceID(r.target.WorkspaceID),
		SessionID:   runtimeapi.SessionID(r.target.SessionID),
	}
}

func (r *SessionRuntime) memoryResearchService(state *runtimeActorState) (*runtimeservice.MemoryResearchService, error) {
	if state == nil || state.memoryResearch == nil || state.memoryResearchErr != nil {
		return nil, runtimeRemoteError(protocol.ErrQueryFailed, r.target, "", "")
	}
	return state.memoryResearch, nil
}

func (r *SessionRuntime) MemoryQuery(ctx context.Context, params protocol.MemoryGetParams, capabilityAvailable bool, beforeRead func() error) (runtimeapi.MemoryView, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(params.RuntimeQuery, beforeRead); err != nil {
			return nil, err
		}
		if !capabilityAvailable {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		service, err := r.memoryResearchService(state)
		if err != nil {
			return nil, err
		}
		result, err := safeMemoryResearchCall(func() (runtimeapi.MemoryView, error) {
			return service.MemoryView(r.runtimeSessionRef(), r.controller)
		})
		return result, r.mapMemoryResearchError(err)
	})
	if err != nil {
		return runtimeapi.MemoryView{}, err
	}
	return value.(runtimeapi.MemoryView), nil
}

func (r *SessionRuntime) MemorySuggestionsQuery(ctx context.Context, params protocol.MemorySuggestionsParams, capabilityAvailable bool, beforeRead func() error) (runtimeapi.MemorySuggestionsView, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(params.RuntimeQuery, beforeRead); err != nil {
			return nil, err
		}
		if !capabilityAvailable {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		service, err := r.memoryResearchService(state)
		if err != nil {
			return nil, err
		}
		result, err := safeMemoryResearchCall(func() (runtimeapi.MemorySuggestionsView, error) {
			return service.Suggestions(r.runtimeSessionRef(), r.controller)
		})
		return result, r.mapMemoryResearchError(err)
	})
	if err != nil {
		return runtimeapi.MemorySuggestionsView{}, err
	}
	return value.(runtimeapi.MemorySuggestionsView), nil
}

func (r *SessionRuntime) ResearchStatusQuery(ctx context.Context, params protocol.ResearchStatusParams, capabilityAvailable bool, beforeRead func() error) (runtimeapi.ResearchStatusView, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(params.RuntimeQuery, beforeRead); err != nil {
			return nil, err
		}
		if !capabilityAvailable {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		service, err := r.memoryResearchService(state)
		if err != nil {
			return nil, err
		}
		access, ok := r.controller.(control.AutoResearchTaskAccess)
		if !ok {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		var summary *autoresearch.Summary
		var available bool
		var queryErr error
		if err := safeControllerCall(func() { summary, available, queryErr = access.CurrentAutoResearchTask() }); err != nil {
			queryErr = err
		}
		if queryErr != nil {
			return nil, runtimeRemoteError(protocol.ErrQueryFailed, r.target, "", "")
		}
		if !available {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		result, err := service.ResearchStatus(r.runtimeSessionRef(), summary)
		return result, r.mapMemoryResearchError(err)
	})
	if err != nil {
		return runtimeapi.ResearchStatusView{}, err
	}
	return value.(runtimeapi.ResearchStatusView), nil
}

func (r *SessionRuntime) ResearchListQuery(ctx context.Context, params protocol.ResearchListParams, capabilityAvailable bool, beforeRead func() error) (runtimeapi.ResearchPage, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(params.RuntimeQuery, beforeRead); err != nil {
			return nil, err
		}
		if !capabilityAvailable {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		service, err := r.memoryResearchService(state)
		if err != nil {
			return nil, err
		}
		access, ok := r.controller.(control.AutoResearchTaskAccess)
		if !ok {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		var summaries []autoresearch.Summary
		var available bool
		var queryErr error
		if err := safeControllerCall(func() { summaries, available, queryErr = access.ListAutoResearchTasks() }); err != nil {
			queryErr = err
		}
		if queryErr != nil {
			return nil, runtimeRemoteError(protocol.ErrQueryFailed, r.target, "", "")
		}
		if !available {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		limit := 0
		if params.Limit != nil {
			limit = *params.Limit
		}
		result, err := service.ResearchList(r.runtimeSessionRef(), summaries, runtimeapi.Cursor(params.Cursor), limit)
		return result, r.mapMemoryResearchError(err)
	})
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	return value.(runtimeapi.ResearchPage), nil
}

func (r *SessionRuntime) ResearchFindingsQuery(ctx context.Context, params protocol.ResearchFindingsParams, capabilityAvailable bool, beforeRead func() error) (runtimeapi.ResearchFindingsPage, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(params.RuntimeQuery, beforeRead); err != nil {
			return nil, err
		}
		if !capabilityAvailable {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		service, err := r.memoryResearchService(state)
		if err != nil {
			return nil, err
		}
		access, ok := r.controller.(control.AutoResearchTaskAccess)
		if !ok {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		storeTaskID, resolveErr := service.ResolveResearchTask(r.runtimeSessionRef(), runtimeapi.ResearchTaskID(params.TaskID))
		if resolveErr != nil {
			return nil, r.mapMemoryResearchError(resolveErr)
		}
		var findings []autoresearch.Finding
		var available bool
		var queryErr error
		if err := safeControllerCall(func() { findings, available, queryErr = access.AutoResearchTaskFindings(storeTaskID) }); err != nil {
			queryErr = err
		}
		if queryErr != nil {
			return nil, runtimeRemoteError(protocol.ErrQueryFailed, r.target, "", "")
		}
		if !available {
			return nil, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
		}
		limit := 0
		if params.Limit != nil {
			limit = *params.Limit
		}
		result, err := service.ResearchFindings(
			r.runtimeSessionRef(), runtimeapi.ResearchTaskID(params.TaskID), findings,
			runtimeapi.Cursor(params.Cursor), limit,
		)
		return result, r.mapMemoryResearchError(err)
	})
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	return value.(runtimeapi.ResearchFindingsPage), nil
}

func (r *SessionRuntime) RememberMemoryMutation(ctx context.Context, registry *idempotency.Registry, params protocol.MemoryRememberParams, capabilityAvailable bool, beforeBegin func() error, onCommitted ...func(runtimeapi.CatalogInvalidation)) (protocol.MemoryRememberResult, error) {
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodMemoryRemember), Target: idempotency.SessionTarget(params.Target), Params: params}
	return memoryResearchMutation(ctx, r, registry, params.SessionMutation, request, capabilityAvailable, beforeBegin, firstInvalidationNotifier(onCommitted),
		func(service *runtimeservice.MemoryResearchService) (protocol.MemoryRememberResult, runtimeapi.CatalogInvalidation, error) {
			result, err := service.Remember(r.runtimeSessionRef(), r.controller, params.Scope, params.Note)
			return protocol.MemoryRememberResult{MemoryID: protocol.MemoryID(result.MemoryID), DisplayPath: result.DisplayPath}, memoryInvalidation(result.InvalidationScope), err
		})
}

func (r *SessionRuntime) ForgetMemoryMutation(ctx context.Context, registry *idempotency.Registry, params protocol.MemoryForgetParams, capabilityAvailable bool, beforeBegin func() error, onCommitted ...func(runtimeapi.CatalogInvalidation)) (protocol.MemoryForgetResult, error) {
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodMemoryForget), Target: idempotency.SessionTarget(params.Target), Params: params}
	return memoryResearchMutation(ctx, r, registry, params.SessionMutation, request, capabilityAvailable, beforeBegin, firstInvalidationNotifier(onCommitted),
		func(service *runtimeservice.MemoryResearchService) (protocol.MemoryForgetResult, runtimeapi.CatalogInvalidation, error) {
			result, err := service.Forget(r.runtimeSessionRef(), r.controller, runtimeapi.MemoryID(params.MemoryID))
			return protocol.MemoryForgetResult{Forgotten: result.Forgotten}, memoryInvalidation(result.InvalidationScope), err
		})
}

func (r *SessionRuntime) SaveMemoryDocumentMutation(ctx context.Context, registry *idempotency.Registry, params protocol.MemoryDocumentSaveParams, capabilityAvailable bool, beforeBegin func() error, onCommitted ...func(runtimeapi.CatalogInvalidation)) (protocol.MemoryDocumentSaveResult, error) {
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodMemoryDocumentSave), Target: idempotency.SessionTarget(params.Target), Params: params}
	return memoryResearchMutation(ctx, r, registry, params.SessionMutation, request, capabilityAvailable, beforeBegin, firstInvalidationNotifier(onCommitted),
		func(service *runtimeservice.MemoryResearchService) (protocol.MemoryDocumentSaveResult, runtimeapi.CatalogInvalidation, error) {
			result, err := service.SaveDocument(r.runtimeSessionRef(), r.controller, runtimeapi.DocumentID(params.DocumentID), params.Body)
			return protocol.MemoryDocumentSaveResult{DocumentID: protocol.DocumentID(result.DocumentID), Saved: result.Saved}, memoryInvalidation(result.InvalidationScope), err
		})
}

func (r *SessionRuntime) AcceptMemorySuggestionMutation(ctx context.Context, registry *idempotency.Registry, params protocol.MemorySuggestionAcceptParams, capabilityAvailable bool, beforeBegin func() error, onCommitted ...func(runtimeapi.CatalogInvalidation)) (protocol.MemorySuggestionAcceptResult, error) {
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodMemorySuggestionAccept), Target: idempotency.SessionTarget(params.Target), Params: params}
	return memoryResearchMutation(ctx, r, registry, params.SessionMutation, request, capabilityAvailable, beforeBegin, firstInvalidationNotifier(onCommitted),
		func(service *runtimeservice.MemoryResearchService) (protocol.MemorySuggestionAcceptResult, runtimeapi.CatalogInvalidation, error) {
			result, err := service.AcceptMemorySuggestion(r.runtimeSessionRef(), r.controller, runtimeapi.SuggestionID(params.SuggestionID), runtimeapi.CatalogRevision(params.ExpectedRevision))
			return protocol.MemorySuggestionAcceptResult{MemoryID: protocol.MemoryID(result.MemoryID)}, memoryInvalidation(result.InvalidationScope), err
		})
}

func (r *SessionRuntime) AcceptSkillSuggestionMutation(ctx context.Context, registry *idempotency.Registry, params protocol.SkillSuggestionAcceptParams, capabilityAvailable bool, beforeBegin func() error, onCommitted ...func(runtimeapi.CatalogInvalidation)) (protocol.SkillSuggestionAcceptResult, error) {
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodSkillSuggestionAccept), Target: idempotency.SessionTarget(params.Target), Params: params}
	return memoryResearchMutation(ctx, r, registry, params.SessionMutation, request, capabilityAvailable, beforeBegin, firstInvalidationNotifier(onCommitted),
		func(service *runtimeservice.MemoryResearchService) (protocol.SkillSuggestionAcceptResult, runtimeapi.CatalogInvalidation, error) {
			result, err := service.AcceptSkillSuggestion(r.runtimeSessionRef(), r.controller, runtimeapi.SuggestionID(params.SuggestionID), runtimeapi.CatalogRevision(params.ExpectedRevision))
			return protocol.SkillSuggestionAcceptResult{SkillID: protocol.SkillID(result.SkillID)}, runtimeapi.CatalogInvalidation{Scope: result.InvalidationScope, Kinds: []runtimeapi.CatalogKind{runtimeapi.CatalogSessionCatalog}}, err
		})
}

func (r *SessionRuntime) RecordResearchEvidenceMutation(ctx context.Context, registry *idempotency.Registry, params protocol.ResearchEvidenceRecordParams, capabilityAvailable bool, beforeBegin func() error, onCommitted ...func(runtimeapi.CatalogInvalidation)) (protocol.ResearchEvidenceRecordResult, error) {
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodResearchEvidenceRecord), Target: idempotency.SessionTarget(params.Target), Params: params}
	return memoryResearchMutation(ctx, r, registry, params.SessionMutation, request, capabilityAvailable, beforeBegin, firstInvalidationNotifier(onCommitted),
		func(service *runtimeservice.MemoryResearchService) (protocol.ResearchEvidenceRecordResult, runtimeapi.CatalogInvalidation, error) {
			access, ok := r.controller.(control.AutoResearchTaskAccess)
			if !ok {
				return protocol.ResearchEvidenceRecordResult{}, runtimeapi.CatalogInvalidation{}, runtimeservice.ErrCapabilityUnavailable
			}
			storeTaskID, storeCriterionID, err := service.ResolveResearchEvidenceTarget(
				r.runtimeSessionRef(), runtimeapi.ResearchTaskID(params.TaskID), runtimeapi.CriterionID(params.CriterionID),
			)
			if err != nil {
				return protocol.ResearchEvidenceRecordResult{}, runtimeapi.CatalogInvalidation{}, err
			}
			finding, err := service.PrepareResearchEvidence(
				r.runtimeSessionRef(), runtimeapi.ResearchTaskID(params.TaskID), runtimeapi.CriterionID(params.CriterionID),
				runtimeapi.ResearchEvidence{
					ID: params.Evidence.ID, Kind: params.Evidence.Kind, Summary: params.Evidence.Summary,
					Source: params.Evidence.Source, Command: params.Evidence.Command,
					Paths: append([]string(nil), params.Evidence.Paths...), Accepted: params.Evidence.Accepted,
				}, time.Now().UTC(),
			)
			if err != nil {
				return protocol.ResearchEvidenceRecordResult{}, runtimeapi.CatalogInvalidation{}, err
			}
			var mutationErr error
			if err := safeControllerCall(func() {
				mutationErr = access.RecordAutoResearchTaskEvidence(storeTaskID, storeCriterionID, finding)
			}); err != nil {
				mutationErr = err
			}
			if mutationErr != nil {
				return protocol.ResearchEvidenceRecordResult{}, runtimeapi.CatalogInvalidation{}, fmt.Errorf("%w: %v", runtimeservice.ErrResearchMutationFailed, mutationErr)
			}
			return protocol.ResearchEvidenceRecordResult{Recorded: true}, runtimeapi.CatalogInvalidation{Scope: runtimeapi.CatalogWorkspace, Kinds: []runtimeapi.CatalogKind{runtimeapi.CatalogResearch}}, nil
		})
}

func memoryInvalidation(scope runtimeapi.CatalogScope) runtimeapi.CatalogInvalidation {
	return runtimeapi.CatalogInvalidation{Scope: scope, Kinds: []runtimeapi.CatalogKind{runtimeapi.CatalogMemory}}
}

func firstInvalidationNotifier(values []func(runtimeapi.CatalogInvalidation)) func(runtimeapi.CatalogInvalidation) {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func memoryResearchMutation[T any](
	ctx context.Context,
	r *SessionRuntime,
	registry *idempotency.Registry,
	mutation protocol.SessionMutation,
	request idempotency.Request,
	capabilityAvailable bool,
	beforeBegin func() error,
	onCommitted func(runtimeapi.CatalogInvalidation),
	apply func(*runtimeservice.MemoryResearchService) (T, runtimeapi.CatalogInvalidation, error),
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
		if !capabilityAvailable {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", ""))
		}
		service, err := r.memoryResearchService(state)
		if err != nil {
			return nil, rejectMutation(claim, err)
		}
		var invalidation runtimeapi.CatalogInvalidation
		result, mutationErr := safeMemoryResearchCall(func() (T, error) {
			applied, proposedInvalidation, applyErr := apply(service)
			invalidation = proposedInvalidation
			return applied, applyErr
		})
		if mutationErr != nil {
			return nil, rejectMutation(claim, r.mapMemoryResearchError(mutationErr))
		}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		if err := claim.Resolve(outcome); err != nil {
			return nil, err
		}
		if onCommitted != nil {
			_ = safeControllerCall(func() { onCommitted(invalidation) })
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
		return zero, errors.New("remote memory/research actor returned an invalid result")
	}
	return result, nil
}

func safeMemoryResearchCall[T any](call func() (T, error)) (result T, err error) {
	panicErr := safeControllerCall(func() { result, err = call() })
	if panicErr != nil {
		return result, panicErr
	}
	return result, err
}

func (r *SessionRuntime) mapMemoryResearchError(err error) error {
	if err == nil {
		return nil
	}
	var remote *protocol.RemoteError
	if errors.As(err, &remote) {
		return remote
	}
	switch {
	case errors.Is(err, runtimeservice.ErrCapabilityUnavailable), errors.Is(err, control.ErrAutoResearchTaskAccessUnavailable):
		return runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "")
	case errors.Is(err, runtimeservice.ErrInvalidCursor):
		return &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "invalid params"}
	case errors.Is(err, runtimeservice.ErrStaleRevision), errors.Is(err, runtimeservice.ErrStaleCursor):
		return runtimeRemoteError(protocol.ErrStaleCursor, r.target, "", "")
	default:
		return runtimeRemoteError(protocol.ErrQueryFailed, r.target, "", "")
	}
}
