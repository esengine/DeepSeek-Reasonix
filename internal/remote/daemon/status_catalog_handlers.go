package daemon

import (
	"context"
	"errors"

	"reasonix/internal/remote/host"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
)

// statusCatalogHandlerSet remains separate so the complete router can merge
// this production domain atomically with other Phase 6 handler families.
func statusCatalogHandlerSet(t *transport) protocol.HandlerSet {
	return protocol.HandlerSet{
		protocol.MethodCatalogSession:    t.handleSessionCatalog,
		protocol.MethodSessionContext:    t.handleSessionContext,
		protocol.MethodSessionBalance:    t.handleSessionBalance,
		protocol.MethodJobList:           t.handleJobList,
		protocol.MethodJobCancel:         t.handleJobCancel,
		protocol.MethodComposerSlashArgs: t.handleComposerSlashArgs,
	}
}

func (t *transport) statusQueryRuntime(query protocol.RuntimeQuery) (*host.SessionRuntime, error) {
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(query.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	return t.currentRuntime(query.Target, query.ExpectedRuntimeEpoch)
}

func (t *transport) handleSessionCatalog(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionCatalogParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.SessionCatalogQuery(ctx, params.RuntimeQuery, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapStatusQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return sessionCatalogToProtocol(result), nil
}

func (t *transport) handleSessionContext(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionContextParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.SessionContextQuery(ctx, params.RuntimeQuery, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapStatusQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.SessionContextResult{Context: contextToProtocol(result)}, nil
}

func (t *transport) handleSessionBalance(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionBalanceParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.SessionBalanceQuery(ctx, params.RuntimeQuery, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapStatusQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.SessionBalanceResult{Available: result.Available, Display: result.Display}, nil
}

func (t *transport) handleJobList(ctx context.Context, value any) (any, error) {
	params := value.(protocol.JobListParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.ListJobsQuery(ctx, params, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapStatusQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.JobListResult{
		Jobs: jobsToProtocol(result.Jobs), HasMore: result.HasMore, NextCursor: protocol.Cursor(result.Next),
	}, nil
}

func (t *transport) handleJobCancel(ctx context.Context, value any) (any, error) {
	params := value.(protocol.JobCancelParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodJobCancel),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.JobCancelResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(
			ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard(),
		)
		return replay, err
	}
	return runtime.CancelJobMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handleComposerSlashArgs(ctx context.Context, value any) (any, error) {
	params := value.(protocol.ComposerSlashArgsParams)
	runtime, err := t.statusQueryRuntime(params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := runtime.ComposerSlashArgsQuery(ctx, params, t.sessionMutationGuard())
	if err != nil {
		return nil, t.mapStatusQueryError(params.Target, params.ExpectedRuntimeEpoch, err)
	}
	items := make([]protocol.SlashArgItem, len(result.Items))
	for index, item := range result.Items {
		items[index] = protocol.SlashArgItem{
			Label: item.Label, Insert: item.Insert, Hint: item.Hint, Descend: item.Descend,
		}
	}
	return protocol.ComposerSlashArgsResult{Items: items, From: result.From}, nil
}

func sessionCatalogToProtocol(value runtimeapi.SessionCatalog) protocol.SessionCatalogResult {
	result := protocol.SessionCatalogResult{
		Revision:   protocol.CatalogRevision(value.Revision),
		Commands:   make([]protocol.CommandCatalogItem, len(value.Commands)),
		MCPServers: make([]protocol.MCPServerCatalogItem, len(value.MCPServers)),
		Skills:     make([]protocol.SkillCatalogItem, len(value.Skills)),
		Plugins:    make([]protocol.PluginCatalogItem, len(value.Plugins)),
	}
	for index, item := range value.Commands {
		result.Commands[index] = protocol.CommandCatalogItem{Name: item.Name, Description: item.Description}
	}
	for index, item := range value.MCPServers {
		result.MCPServers[index] = protocol.MCPServerCatalogItem{Name: item.Name, Available: item.Available, ToolCount: item.ToolCount}
	}
	for index, item := range value.Skills {
		result.Skills[index] = protocol.SkillCatalogItem{
			ID: protocol.SkillID(item.ID), Name: item.Name, Description: item.Description, Scope: item.Scope,
		}
	}
	for index, item := range value.Plugins {
		result.Plugins[index] = protocol.PluginCatalogItem{ID: item.ID, Name: item.Name, Enabled: item.Enabled}
	}
	return result
}

func contextToProtocol(value runtimeapi.ContextView) protocol.ContextView {
	result := protocol.ContextView{
		UsedTokens: value.UsedTokens, WindowTokens: value.WindowTokens,
		PromptTokens: value.PromptTokens, CompletionTokens: value.CompletionTokens,
		TotalTokens: value.TotalTokens, ReasoningTokens: value.ReasoningTokens,
		CacheHitTokens: value.CacheHitTokens, CacheMissTokens: value.CacheMissTokens,
		SessionCacheHitTokens:   value.SessionCacheHitTokens,
		SessionCacheMissTokens:  value.SessionCacheMissTokens,
		SessionCompletionTokens: value.SessionCompletionTokens,
		RequestCount:            value.RequestCount, ElapsedMs: value.ElapsedMillis,
		SessionCost: value.SessionCost, SessionCurrency: value.SessionCurrency,
		Sources:   make([]protocol.UsageSourceView, len(value.Sources)),
		ReadFiles: make([]protocol.ReadFileRecord, len(value.ReadFiles)),
	}
	for index, source := range value.Sources {
		result.Sources[index] = protocol.UsageSourceView{
			Source: source.Source, PromptTokens: source.PromptTokens,
			CompletionTokens: source.CompletionTokens, TotalTokens: source.TotalTokens,
			ReasoningTokens: source.ReasoningTokens, CacheHitTokens: source.CacheHitTokens,
			CacheMissTokens: source.CacheMissTokens, RequestCount: source.RequestCount,
			SessionCost: source.SessionCost, SessionCurrency: source.SessionCurrency,
		}
	}
	for index, record := range value.ReadFiles {
		result.ReadFiles[index] = protocol.ReadFileRecord{
			Path: record.Path, Turn: record.Turn, TimeMs: record.TimeMs,
			Offset: record.Offset, Limit: record.Limit, Truncated: record.Truncated,
		}
	}
	return result
}

func jobsToProtocol(values []runtimeapi.Job) []protocol.JobView {
	result := make([]protocol.JobView, len(values))
	for index, item := range values {
		result[index] = protocol.JobView{
			ID: protocol.JobID(item.ID), Kind: protocol.JobKind(item.Kind), Label: item.Label,
			Status: protocol.JobStatus(item.Status), StartedAt: item.StartedAtMillis,
		}
	}
	return result
}

func (t *transport) mapStatusQueryError(target protocol.RuntimeTarget, expected protocol.RuntimeEpoch, err error) error {
	if err == nil {
		return nil
	}
	var remote *protocol.RemoteError
	if errors.As(err, &remote) {
		return err
	}
	switch {
	case errors.Is(err, host.ErrRuntimeClosed), errors.Is(err, host.ErrRuntimeManagerClosed):
		return t.mapRuntimeError(target, expected, err)
	case errors.Is(err, runtimeservice.ErrInvalidCursor), errors.Is(err, runtimeservice.ErrStaleCursor):
		targetCopy := target
		return protocol.MustRemoteError(protocol.ErrStaleCursor, protocol.ErrorOptions{Target: &targetCopy})
	case errors.Is(err, runtimeservice.ErrInvalidStatusProjection),
		errors.Is(err, runtimeservice.ErrInvalidCatalogProjection),
		errors.Is(err, runtimeservice.ErrInvalidComposerProjection),
		errors.Is(err, runtimeservice.ErrInvalidSession),
		errors.Is(err, runtimeservice.ErrQueryFailed):
		targetCopy := target
		return protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{Target: &targetCopy})
	default:
		t.server.reportInternal(protocol.MethodSessionContext, err)
		targetCopy := target
		return protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{Target: &targetCopy})
	}
}
