package daemon

import (
	"context"
	"errors"

	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/runtimeapi"
)

// memoryResearchHandlerSet is a complete production handler family. server.go
// only needs to merge these eleven entries into the frozen complete router.
func memoryResearchHandlerSet(t *transport) protocol.HandlerSet {
	return protocol.HandlerSet{
		protocol.MethodMemoryGet:              t.handleMemoryGet,
		protocol.MethodMemorySuggestions:      t.handleMemorySuggestions,
		protocol.MethodMemoryRemember:         t.handleMemoryRemember,
		protocol.MethodMemoryForget:           t.handleMemoryForget,
		protocol.MethodMemoryDocumentSave:     t.handleMemoryDocumentSave,
		protocol.MethodMemorySuggestionAccept: t.handleMemorySuggestionAccept,
		protocol.MethodSkillSuggestionAccept:  t.handleSkillSuggestionAccept,
		protocol.MethodResearchStatus:         t.handleResearchStatus,
		protocol.MethodResearchList:           t.handleResearchList,
		protocol.MethodResearchFindings:       t.handleResearchFindings,
		protocol.MethodResearchEvidenceRecord: t.handleResearchEvidenceRecord,
	}
}

func (t *transport) handleMemoryGet(ctx context.Context, value any) (any, error) {
	params := value.(protocol.MemoryGetParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.MemoryQuery(ctx, params, t.server.capabilities.Features.Memory, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapMemoryResearchQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return memoryViewToProtocol(result), nil
}

func (t *transport) handleMemorySuggestions(ctx context.Context, value any) (any, error) {
	params := value.(protocol.MemorySuggestionsParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.MemorySuggestionsQuery(ctx, params, t.server.capabilities.Features.Memory, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapMemoryResearchQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return memorySuggestionsToProtocol(result), nil
}

func (t *transport) handleMemoryRemember(ctx context.Context, value any) (any, error) {
	params := value.(protocol.MemoryRememberParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := memoryResearchRequest(protocol.MethodMemoryRemember, params.RequestID, params.Target, params)
	var replay protocol.MemoryRememberResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.RememberMemoryMutation(ctx, t.server.requests, params, t.server.capabilities.Features.Memory, t.sessionMutationGuard(), t.memoryResearchNotifier(params.Target.WorkspaceID))
}

func (t *transport) handleMemoryForget(ctx context.Context, value any) (any, error) {
	params := value.(protocol.MemoryForgetParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := memoryResearchRequest(protocol.MethodMemoryForget, params.RequestID, params.Target, params)
	var replay protocol.MemoryForgetResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.ForgetMemoryMutation(ctx, t.server.requests, params, t.server.capabilities.Features.Memory, t.sessionMutationGuard(), t.memoryResearchNotifier(params.Target.WorkspaceID))
}

func (t *transport) handleMemoryDocumentSave(ctx context.Context, value any) (any, error) {
	params := value.(protocol.MemoryDocumentSaveParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := memoryResearchRequest(protocol.MethodMemoryDocumentSave, params.RequestID, params.Target, params)
	var replay protocol.MemoryDocumentSaveResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.SaveMemoryDocumentMutation(ctx, t.server.requests, params, t.server.capabilities.Features.Memory, t.sessionMutationGuard(), t.memoryResearchNotifier(params.Target.WorkspaceID))
}

func (t *transport) handleMemorySuggestionAccept(ctx context.Context, value any) (any, error) {
	params := value.(protocol.MemorySuggestionAcceptParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := memoryResearchRequest(protocol.MethodMemorySuggestionAccept, params.RequestID, params.Target, params)
	var replay protocol.MemorySuggestionAcceptResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.AcceptMemorySuggestionMutation(ctx, t.server.requests, params, t.server.capabilities.Features.Memory, t.sessionMutationGuard(), t.memoryResearchNotifier(params.Target.WorkspaceID))
}

func (t *transport) handleSkillSuggestionAccept(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SkillSuggestionAcceptParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := memoryResearchRequest(protocol.MethodSkillSuggestionAccept, params.RequestID, params.Target, params)
	var replay protocol.SkillSuggestionAcceptResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.AcceptSkillSuggestionMutation(ctx, t.server.requests, params, t.server.capabilities.Features.Memory, t.sessionMutationGuard(), t.memoryResearchNotifier(params.Target.WorkspaceID))
}

func (t *transport) handleResearchStatus(ctx context.Context, value any) (any, error) {
	params := value.(protocol.ResearchStatusParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.ResearchStatusQuery(ctx, params, t.server.capabilities.Features.Research, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapMemoryResearchQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return researchStatusToProtocol(result), nil
}

func (t *transport) handleResearchList(ctx context.Context, value any) (any, error) {
	params := value.(protocol.ResearchListParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.ResearchListQuery(ctx, params, t.server.capabilities.Features.Research, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapMemoryResearchQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.ResearchListResult{
		Items: researchTasksToProtocol(result.Items), HasMore: result.HasMore, NextCursor: protocol.Cursor(result.Next),
	}, nil
}

func (t *transport) handleResearchFindings(ctx context.Context, value any) (any, error) {
	params := value.(protocol.ResearchFindingsParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.ResearchFindingsQuery(ctx, params, t.server.capabilities.Features.Research, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapMemoryResearchQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.ResearchFindingsResult{
		Items: researchFindingsToProtocol(result.Items), HasMore: result.HasMore, NextCursor: protocol.Cursor(result.Next),
	}, nil
}

func (t *transport) handleResearchEvidenceRecord(ctx context.Context, value any) (any, error) {
	params := value.(protocol.ResearchEvidenceRecordParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := memoryResearchRequest(protocol.MethodResearchEvidenceRecord, params.RequestID, params.Target, params)
	var replay protocol.ResearchEvidenceRecordResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.RecordResearchEvidenceMutation(ctx, t.server.requests, params, t.server.capabilities.Features.Research, t.sessionMutationGuard(), t.memoryResearchNotifier(params.Target.WorkspaceID))
}

func (t *transport) mapMemoryResearchQueryError(target protocol.RuntimeTarget, expected protocol.RuntimeEpoch, err error) error {
	var rpcErr *rpcwire.RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	return t.mapStatusQueryError(target, expected, err)
}

func (t *transport) memoryResearchNotifier(workspaceID protocol.WorkspaceID) func(runtimeapi.CatalogInvalidation) {
	return func(invalidation runtimeapi.CatalogInvalidation) {
		t.server.catalog.AdvanceRevision()
		scope := protocol.CatalogScope(invalidation.Scope)
		var affected []protocol.WorkspaceID
		if scope == protocol.CatalogWorkspace {
			affected = []protocol.WorkspaceID{workspaceID}
		}
		kinds := make([]protocol.CatalogKind, len(invalidation.Kinds))
		for index, kind := range invalidation.Kinds {
			kinds[index] = protocol.CatalogKind(kind)
		}
		t.notifyCatalogChanged(scope, affected, kinds...)
	}
}

func memoryResearchRequest(method protocol.Method, requestID protocol.RequestID, target protocol.RuntimeTarget, params any) idempotency.Request {
	return idempotency.Request{RequestID: requestID, Method: string(method), Target: idempotency.SessionTarget(target), Params: params}
}

func memoryViewToProtocol(value runtimeapi.MemoryView) protocol.MemoryGetResult {
	result := protocol.MemoryGetResult{
		Revision: protocol.CatalogRevision(value.Revision), Available: value.Available,
		Documents: make([]protocol.MemoryDocument, len(value.Documents)),
		Facts:     make([]protocol.MemoryFact, len(value.Facts)), Archives: make([]protocol.MemoryArchive, len(value.Archives)),
		Scopes: make([]protocol.MemoryScope, len(value.Scopes)),
	}
	for index, item := range value.Documents {
		result.Documents[index] = protocol.MemoryDocument{
			DocumentID: protocol.DocumentID(item.DocumentID), Scope: item.Scope,
			Body: cloneString(item.Body), DisplayPath: item.DisplayPath,
		}
	}
	for index, item := range value.Facts {
		result.Facts[index] = memoryFactToProtocol(item)
	}
	for index, item := range value.Archives {
		result.Archives[index] = protocol.MemoryArchive{MemoryFact: memoryFactToProtocol(item.MemoryFact), ArchivedAt: item.ArchivedAt}
	}
	for index, item := range value.Scopes {
		result.Scopes[index] = protocol.MemoryScope{Scope: item.Scope, DisplayPath: item.DisplayPath}
	}
	return result
}

func memoryFactToProtocol(value runtimeapi.MemoryFact) protocol.MemoryFact {
	return protocol.MemoryFact{
		MemoryID: protocol.MemoryID(value.MemoryID), Name: value.Name, Title: value.Title,
		Description: value.Description, Type: value.Type, Body: cloneString(value.Body),
	}
}

func memorySuggestionsToProtocol(value runtimeapi.MemorySuggestionsView) protocol.MemorySuggestionsResult {
	result := protocol.MemorySuggestionsResult{
		Revision: protocol.CatalogRevision(value.Revision), Available: value.Available,
		Memories: make([]protocol.MemorySuggestion, len(value.Memories)), Skills: make([]protocol.SkillSuggestion, len(value.Skills)),
	}
	for index, item := range value.Memories {
		result.Memories[index] = protocol.MemorySuggestion{
			SuggestionID: protocol.SuggestionID(item.SuggestionID), Name: item.Name, Title: item.Title,
			Description: item.Description, Type: item.Type, Body: cloneString(item.Body), Reason: item.Reason,
			Evidence: append([]string(nil), item.Evidence...),
		}
	}
	for index, item := range value.Skills {
		result.Skills[index] = protocol.SkillSuggestion{
			SuggestionID: protocol.SuggestionID(item.SuggestionID), Name: item.Name, Description: item.Description,
			Scope: item.Scope, Body: cloneString(item.Body), Reason: item.Reason,
			Evidence: append([]string(nil), item.Evidence...),
		}
	}
	return result
}

func researchStatusToProtocol(value runtimeapi.ResearchStatusView) protocol.ResearchStatusResult {
	result := protocol.ResearchStatusResult{Available: value.Available}
	if value.Task != nil {
		task := researchTaskToProtocol(*value.Task)
		result.Task = &task
	}
	return result
}

func researchTasksToProtocol(values []runtimeapi.ResearchTask) []protocol.ResearchTask {
	result := make([]protocol.ResearchTask, len(values))
	for index, item := range values {
		result[index] = researchTaskToProtocol(item)
	}
	return result
}

func researchTaskToProtocol(value runtimeapi.ResearchTask) protocol.ResearchTask {
	criteria := make([]protocol.ResearchCriterion, len(value.OpenCriteria))
	for index, item := range value.OpenCriteria {
		criteria[index] = protocol.ResearchCriterion{
			CriterionID: protocol.CriterionID(item.CriterionID), Description: item.Description,
			Required: item.Required, EvidenceCount: item.EvidenceCount, Status: item.Status,
		}
	}
	return protocol.ResearchTask{
		TaskID: protocol.ResearchTaskID(value.TaskID), Goal: cloneString(value.Goal), Status: value.Status,
		Iteration: value.Iteration, CurrentDirection: cloneString(value.CurrentDirection),
		StaleCount: value.StaleCount, PivotCount: value.PivotCount, PivotRequired: value.PivotRequired,
		LastHeartbeatAt: value.LastHeartbeatAt, FindingCount: value.FindingCount, OpenCriteria: criteria,
		Blocker: cloneString(value.Blocker), DisplayPath: value.DisplayPath,
		NextRequiredAction: cloneString(value.NextRequiredAction),
	}
}

func researchFindingsToProtocol(values []runtimeapi.ResearchFinding) []protocol.ResearchFinding {
	result := make([]protocol.ResearchFinding, len(values))
	for index, item := range values {
		result[index] = protocol.ResearchFinding{
			ID: item.ID, Kind: item.Kind, Summary: cloneString(item.Summary), Source: item.Source,
			Command: item.Command, Paths: append([]string(nil), item.Paths...), Accepted: item.Accepted, CreatedAt: item.CreatedAt,
		}
	}
	return result
}
