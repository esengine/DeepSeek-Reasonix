package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	remoteclient "reasonix/internal/remote/client"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeapi"
)

var _ runtimeapi.V1RuntimeAPI = (*RemoteRuntimeAdapter)(nil)

func remoteLimit(value int) *int {
	if value == 0 {
		return nil
	}
	copy := value
	return &copy
}

func remoteResult[T any](value any, method protocol.Method) (T, error) {
	result, ok := value.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s returned %T", method, value)
	}
	return result, nil
}

func (a *RemoteRuntimeAdapter) runtimeQuery(session runtimeapi.SessionRef) (protocol.RuntimeQuery, error) {
	identity, err := a.sessionMutationIdentity(session)
	if err != nil {
		return protocol.RuntimeQuery{}, err
	}
	return protocol.RuntimeQuery{
		ExpectedHostEpoch: identity.hostEpoch, Target: identity.target,
		ExpectedRuntimeEpoch: identity.runtimeEpoch,
	}, nil
}

func (a *RemoteRuntimeAdapter) sessionRecordIdentity(session runtimeapi.SessionRef) (remoteclient.Status, protocol.RuntimeTarget, error) {
	if !session.Valid() {
		return remoteclient.Status{}, protocol.RuntimeTarget{}, errors.New("Remote Session identity is required")
	}
	status, err := a.connectedStatus()
	if err != nil {
		return remoteclient.Status{}, protocol.RuntimeTarget{}, err
	}
	return status, mapRuntimeSessionRef(session), nil
}

func remoteRecordMutation(requestID protocol.RequestID, status remoteclient.Status, target protocol.RuntimeTarget) protocol.SessionRecordMutation {
	return protocol.SessionRecordMutation{
		RequestID: requestID, ExpectedHostEpoch: status.HostEpoch, Target: target,
	}
}

func (a *RemoteRuntimeAdapter) HostCapabilities(ctx context.Context) (runtimeapi.Capabilities, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.Capabilities{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodHostCapabilities, protocol.HostCapabilitiesParams{ExpectedHostEpoch: status.HostEpoch})
	if err != nil {
		return runtimeapi.Capabilities{}, err
	}
	result, err := remoteResult[protocol.HostCapabilitiesResult](value, protocol.MethodHostCapabilities)
	if err != nil {
		return runtimeapi.Capabilities{}, err
	}
	if result.HostEpoch != status.HostEpoch {
		return runtimeapi.Capabilities{}, errors.New("host/capabilities returned a different Host epoch")
	}
	return mapRemoteCapabilities(result.Capabilities), nil
}

func (a *RemoteRuntimeAdapter) HostConfigSummary(ctx context.Context) (runtimeapi.HostConfigSummary, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.HostConfigSummary{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodHostConfigSummary, protocol.HostConfigSummaryParams{ExpectedHostEpoch: status.HostEpoch})
	if err != nil {
		return runtimeapi.HostConfigSummary{}, err
	}
	result, err := remoteResult[protocol.HostConfigSummaryResult](value, protocol.MethodHostConfigSummary)
	if err != nil {
		return runtimeapi.HostConfigSummary{}, err
	}
	summary := runtimeapi.HostConfigSummary{
		Available: true, Revision: runtimeapi.CatalogRevision(result.Revision),
		EffectiveScopes: make([]runtimeapi.EffectiveScope, len(result.EffectiveScopes)),
		DisplayPaths:    make([]runtimeapi.ConfigDisplayPath, len(result.DisplayPaths)),
		FeatureStates:   make([]runtimeapi.FeatureState, len(result.FeatureStates)),
		CLIHints:        make([]runtimeapi.CLIHint, len(result.CLIHints)),
		Models:          []string{}, CollaborationModes: []string{}, TokenModes: []string{}, ToolApprovalModes: []string{},
	}
	for i, item := range result.EffectiveScopes {
		summary.EffectiveScopes[i] = runtimeapi.EffectiveScope{Name: item.Name, Active: item.Active}
	}
	for i, item := range result.DisplayPaths {
		summary.DisplayPaths[i] = runtimeapi.ConfigDisplayPath{Scope: item.Scope, DisplayPath: item.DisplayPath}
	}
	for i, item := range result.FeatureStates {
		summary.FeatureStates[i] = runtimeapi.FeatureState{Feature: item.Feature, Available: item.Available, Summary: item.Summary}
	}
	for i, item := range result.CLIHints {
		summary.CLIHints[i] = runtimeapi.CLIHint{Label: item.Label, Command: item.Command}
	}
	return summary, nil
}

func (a *RemoteRuntimeAdapter) ListWorkspaces(ctx context.Context, input runtimeapi.ListWorkspacesInput) (runtimeapi.WorkspaceListPage, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.WorkspaceListPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodWorkspaceList, protocol.WorkspaceListParams{
		ExpectedHostEpoch: status.HostEpoch, Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.WorkspaceListPage{}, err
	}
	result, err := remoteResult[protocol.WorkspaceListResult](value, protocol.MethodWorkspaceList)
	if err != nil {
		return runtimeapi.WorkspaceListPage{}, err
	}
	items := make([]runtimeapi.Workspace, len(result.Items))
	for i, item := range result.Items {
		items[i] = runtimeapi.Workspace{ID: runtimeapi.WorkspaceID(item.WorkspaceID), Name: item.Name, DisplayPath: item.DisplayPath}
	}
	return runtimeapi.WorkspaceListPage{Items: items, HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func (a *RemoteRuntimeAdapter) CloseWorkspace(ctx context.Context, input runtimeapi.CloseWorkspaceInput) (runtimeapi.CloseWorkspaceResult, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.CloseWorkspaceResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodWorkspaceClose, status.HostEpoch, protocol.RuntimeTarget{}, "", input)
	if err != nil {
		return runtimeapi.CloseWorkspaceResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodWorkspaceClose, protocol.WorkspaceCloseParams{
		HostMutation: protocol.HostMutation{RequestID: attempt.id(), ExpectedHostEpoch: status.HostEpoch},
		WorkspaceID:  protocol.WorkspaceID(input.WorkspaceID),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.CloseWorkspaceResult{}, err
	}
	result, err := remoteResult[protocol.WorkspaceCloseResult](value, protocol.MethodWorkspaceClose)
	if err != nil {
		return runtimeapi.CloseWorkspaceResult{}, err
	}
	return runtimeapi.CloseWorkspaceResult{Disposition: runtimeapi.WorkspaceCloseDisposition(result.Disposition)}, nil
}

func (a *RemoteRuntimeAdapter) WorkspaceCatalog(ctx context.Context, input runtimeapi.WorkspaceCatalogInput) (runtimeapi.WorkspaceCatalog, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.WorkspaceCatalog{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodCatalogWorkspace, protocol.WorkspaceCatalogParams{
		ExpectedHostEpoch: status.HostEpoch, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID),
	})
	if err != nil {
		return runtimeapi.WorkspaceCatalog{}, err
	}
	result, err := remoteResult[protocol.WorkspaceCatalogResult](value, protocol.MethodCatalogWorkspace)
	if err != nil {
		return runtimeapi.WorkspaceCatalog{}, err
	}
	models := make([]runtimeapi.ModelCatalogItem, len(result.Models))
	for i, item := range result.Models {
		models[i] = runtimeapi.ModelCatalogItem{
			Ref: runtimeapi.ModelRef(item.Ref), Provider: item.Provider, Model: item.Model,
			Effort: runtimeapi.EffortCatalog{Supported: item.Effort.Supported, Default: item.Effort.Default, Levels: append([]string(nil), item.Effort.Levels...)},
		}
	}
	return runtimeapi.WorkspaceCatalog{
		Revision: runtimeapi.CatalogRevision(result.Revision), Models: models,
		CollaborationModes: mapCollaborationModes(result.CollaborationModes), TokenModes: mapTokenModes(result.TokenModes),
		ToolApprovalModes: mapApprovalModes(result.ToolApprovalModes), DefaultProfile: mapRemoteResolvedProfile(result.DefaultProfile),
	}, nil
}

func mapCollaborationModes(values []protocol.CollaborationMode) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func mapTokenModes(values []protocol.TokenMode) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func mapApprovalModes(values []protocol.ToolApprovalMode) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func (a *RemoteRuntimeAdapter) SessionCatalog(ctx context.Context, input runtimeapi.SessionCatalogInput) (runtimeapi.SessionCatalog, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodCatalogSession, protocol.SessionCatalogParams{RuntimeQuery: query})
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	result, err := remoteResult[protocol.SessionCatalogResult](value, protocol.MethodCatalogSession)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	out := runtimeapi.SessionCatalog{
		Revision:   runtimeapi.CatalogRevision(result.Revision),
		Commands:   make([]runtimeapi.CommandCatalogItem, len(result.Commands)),
		MCPServers: make([]runtimeapi.MCPServerCatalogItem, len(result.MCPServers)),
		Skills:     make([]runtimeapi.SkillCatalogItem, len(result.Skills)), Plugins: make([]runtimeapi.PluginCatalogItem, len(result.Plugins)),
	}
	for i, item := range result.Commands {
		out.Commands[i] = runtimeapi.CommandCatalogItem{Name: item.Name, Description: item.Description}
	}
	for i, item := range result.MCPServers {
		out.MCPServers[i] = runtimeapi.MCPServerCatalogItem{Name: item.Name, Available: item.Available, ToolCount: item.ToolCount}
	}
	for i, item := range result.Skills {
		out.Skills[i] = runtimeapi.SkillCatalogItem{ID: runtimeapi.SkillID(item.ID), Name: item.Name, Description: item.Description, Scope: item.Scope}
	}
	for i, item := range result.Plugins {
		out.Plugins[i] = runtimeapi.PluginCatalogItem{ID: item.ID, Name: item.Name, Enabled: item.Enabled}
	}
	return out, nil
}

func (a *RemoteRuntimeAdapter) ListTopics(ctx context.Context, input runtimeapi.ListTopicsInput) (runtimeapi.TopicPage, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.TopicPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodTopicList, protocol.TopicListParams{
		ExpectedHostEpoch: status.HostEpoch, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID),
		Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.TopicPage{}, err
	}
	result, err := remoteResult[protocol.TopicListResult](value, protocol.MethodTopicList)
	if err != nil {
		return runtimeapi.TopicPage{}, err
	}
	items := make([]runtimeapi.TopicSummary, len(result.Items))
	for i, item := range result.Items {
		items[i] = runtimeapi.TopicSummary{TopicID: runtimeapi.TopicID(item.TopicID), Title: item.Title, CreatedAtMillis: item.CreatedAtMs, SessionCount: item.SessionCount, LastActivityMillis: item.LastActivityAtMs}
	}
	return runtimeapi.TopicPage{Items: items, HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func (a *RemoteRuntimeAdapter) CreateTopic(ctx context.Context, input runtimeapi.CreateTopicInput) (runtimeapi.CreatedTopic, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.CreatedTopic{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodTopicCreate, status.HostEpoch, protocol.RuntimeTarget{}, "", input)
	if err != nil {
		return runtimeapi.CreatedTopic{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodTopicCreate, protocol.TopicCreateParams{
		HostMutation: protocol.HostMutation{RequestID: attempt.id(), ExpectedHostEpoch: status.HostEpoch}, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID), Title: input.Title,
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.CreatedTopic{}, err
	}
	result, err := remoteResult[protocol.TopicCreateResult](value, protocol.MethodTopicCreate)
	if err != nil {
		return runtimeapi.CreatedTopic{}, err
	}
	return runtimeapi.CreatedTopic{TopicID: runtimeapi.TopicID(result.TopicID), Title: result.Title, CreatedAtMillis: result.CreatedAtMs, SessionCount: result.SessionCount}, nil
}

func (a *RemoteRuntimeAdapter) RenameTopic(ctx context.Context, input runtimeapi.RenameTopicInput) (runtimeapi.RenameTopicResult, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.RenameTopicResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodTopicRename, status.HostEpoch, protocol.RuntimeTarget{}, "", input)
	if err != nil {
		return runtimeapi.RenameTopicResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodTopicRename, protocol.TopicRenameParams{
		HostMutation: protocol.HostMutation{RequestID: attempt.id(), ExpectedHostEpoch: status.HostEpoch}, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID), TopicID: protocol.TopicID(input.TopicID), Title: input.Title,
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.RenameTopicResult{}, err
	}
	result, err := remoteResult[protocol.TopicRenameResult](value, protocol.MethodTopicRename)
	if err != nil {
		return runtimeapi.RenameTopicResult{}, err
	}
	return runtimeapi.RenameTopicResult{Title: result.Title}, nil
}

func (a *RemoteRuntimeAdapter) DeleteTopic(ctx context.Context, input runtimeapi.DeleteTopicInput) (runtimeapi.DeleteTopicResult, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.DeleteTopicResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodTopicDelete, status.HostEpoch, protocol.RuntimeTarget{}, "", input)
	if err != nil {
		return runtimeapi.DeleteTopicResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodTopicDelete, protocol.TopicDeleteParams{
		HostMutation: protocol.HostMutation{RequestID: attempt.id(), ExpectedHostEpoch: status.HostEpoch}, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID), TopicID: protocol.TopicID(input.TopicID),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.DeleteTopicResult{}, err
	}
	result, err := remoteResult[protocol.TopicDeleteResult](value, protocol.MethodTopicDelete)
	if err != nil {
		return runtimeapi.DeleteTopicResult{}, err
	}
	return runtimeapi.DeleteTopicResult{Deleted: result.Deleted}, nil
}

func (a *RemoteRuntimeAdapter) TrashTopic(ctx context.Context, input runtimeapi.TrashTopicInput) (runtimeapi.TrashTopicResult, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.TrashTopicResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodTopicTrash, status.HostEpoch, protocol.RuntimeTarget{}, "", input)
	if err != nil {
		return runtimeapi.TrashTopicResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodTopicTrash, protocol.TopicTrashParams{
		HostMutation: protocol.HostMutation{RequestID: attempt.id(), ExpectedHostEpoch: status.HostEpoch}, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID), TopicID: protocol.TopicID(input.TopicID),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.TrashTopicResult{}, err
	}
	result, err := remoteResult[protocol.TopicTrashResult](value, protocol.MethodTopicTrash)
	if err != nil {
		return runtimeapi.TrashTopicResult{}, err
	}
	return runtimeapi.TrashTopicResult{Disposition: runtimeapi.CleanupDisposition(result.Disposition), TrashedSessions: result.TrashedSessions}, nil
}

func (a *RemoteRuntimeAdapter) ListSessions(ctx context.Context, input runtimeapi.ListSessionsInput) (runtimeapi.SessionListPage, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.SessionListPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: status.HostEpoch, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID),
		Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.SessionListPage{}, err
	}
	result, err := remoteResult[protocol.SessionListResult](value, protocol.MethodSessionList)
	if err != nil {
		return runtimeapi.SessionListPage{}, err
	}
	items := make([]runtimeapi.SessionSummary, len(result.Items))
	for i, item := range result.Items {
		mapped := runtimeapi.SessionSummary{
			Session: mapRemoteSessionRef(item.Target), TopicID: runtimeapi.TopicID(item.TopicID), Title: item.Title,
			Preview: item.Preview, Turns: item.Turns, CreatedAtMillis: item.CreatedAtMs,
			LastActivityMillis: item.LastActivityAtMs, RecoveryInterrupted: item.RecoveryInterrupted,
		}
		if item.BranchSource != nil {
			mapped.BranchSource = &runtimeapi.BranchSource{
				Parent:             mapRemoteSessionRef(item.BranchSource.ParentTarget),
				ParentCheckpointID: runtimeapi.CheckpointID(item.BranchSource.ParentCheckpointID),
			}
		}
		if item.Runtime != nil {
			mapped.Runtime = &runtimeapi.SessionRuntimeSummary{
				Running: item.Runtime.Running, PendingPrompt: item.Runtime.PendingPrompt, ActiveJobs: item.Runtime.ActiveJobs,
			}
		}
		items[i] = mapped
	}
	return runtimeapi.SessionListPage{Items: items, HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func (a *RemoteRuntimeAdapter) CloseSession(ctx context.Context, input runtimeapi.CloseSessionInput) (runtimeapi.CloseSessionResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.CloseSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionClose, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.CloseSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionClose, protocol.SessionCloseParams{SessionMutation: identity.mutation(attempt.id())})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.CloseSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionCloseResult](value, protocol.MethodSessionClose)
	if err != nil {
		return runtimeapi.CloseSessionResult{}, err
	}
	if result.Disposition != protocol.SessionRetainedActive {
		a.mu.Lock()
		delete(a.sessions, identity.target)
		a.mu.Unlock()
	}
	return runtimeapi.CloseSessionResult{Disposition: runtimeapi.SessionCloseDisposition(result.Disposition)}, nil
}

func (a *RemoteRuntimeAdapter) RenameSession(ctx context.Context, input runtimeapi.RenameSessionInput) (runtimeapi.RenameSessionResult, error) {
	status, target, err := a.sessionRecordIdentity(input.Session)
	if err != nil {
		return runtimeapi.RenameSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionRename, status.HostEpoch, target, "", input)
	if err != nil {
		return runtimeapi.RenameSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionRename, protocol.SessionRenameParams{
		SessionRecordMutation: remoteRecordMutation(attempt.id(), status, target), Title: input.Title,
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.RenameSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionRenameResult](value, protocol.MethodSessionRename)
	if err != nil {
		return runtimeapi.RenameSessionResult{}, err
	}
	return runtimeapi.RenameSessionResult{Title: result.Title}, nil
}

func (a *RemoteRuntimeAdapter) ListTrashedSessions(ctx context.Context, input runtimeapi.ListTrashedSessionsInput) (runtimeapi.TrashPage, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.TrashPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodSessionTrashList, protocol.SessionTrashListParams{
		ExpectedHostEpoch: status.HostEpoch, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID),
		Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.TrashPage{}, err
	}
	result, err := remoteResult[protocol.SessionTrashListResult](value, protocol.MethodSessionTrashList)
	if err != nil {
		return runtimeapi.TrashPage{}, err
	}
	items := make([]runtimeapi.TrashEntry, len(result.Items))
	for i, item := range result.Items {
		items[i] = runtimeapi.TrashEntry{
			Session: mapRemoteSessionRef(item.Target), TopicID: runtimeapi.TopicID(item.TopicID), Title: item.Title,
			Preview: item.Preview, TrashedAtMillis: item.TrashedAtMs, RecoveryCopy: item.RecoveryCopy,
		}
	}
	return runtimeapi.TrashPage{Items: items, HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func (a *RemoteRuntimeAdapter) TrashSession(ctx context.Context, input runtimeapi.TrashSessionInput) (runtimeapi.TrashSessionResult, error) {
	status, target, err := a.sessionRecordIdentity(input.Session)
	if err != nil {
		return runtimeapi.TrashSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionTrash, status.HostEpoch, target, "", input)
	if err != nil {
		return runtimeapi.TrashSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionTrash, protocol.SessionTrashParams{
		SessionRecordMutation: remoteRecordMutation(attempt.id(), status, target), Guard: protocol.TrashGuard(input.Guard),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.TrashSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionTrashResult](value, protocol.MethodSessionTrash)
	if err != nil {
		return runtimeapi.TrashSessionResult{}, err
	}
	a.mu.Lock()
	delete(a.sessions, target)
	a.mu.Unlock()
	return runtimeapi.TrashSessionResult{Disposition: runtimeapi.CleanupDisposition(result.Disposition)}, nil
}

func (a *RemoteRuntimeAdapter) RestoreSession(ctx context.Context, input runtimeapi.RestoreSessionInput) (runtimeapi.RestoreSessionResult, error) {
	status, target, err := a.sessionRecordIdentity(input.Session)
	if err != nil {
		return runtimeapi.RestoreSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionRestore, status.HostEpoch, target, "", input)
	if err != nil {
		return runtimeapi.RestoreSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionRestore, protocol.SessionRestoreParams{
		SessionRecordMutation: remoteRecordMutation(attempt.id(), status, target),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.RestoreSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionRestoreResult](value, protocol.MethodSessionRestore)
	if err != nil {
		return runtimeapi.RestoreSessionResult{}, err
	}
	return runtimeapi.RestoreSessionResult{
		Session: mapRemoteSessionRef(result.Target), TopicID: runtimeapi.TopicID(result.TopicID),
		Disposition: runtimeapi.SessionRestoreDisposition(result.Disposition),
	}, nil
}

func (a *RemoteRuntimeAdapter) PurgeSession(ctx context.Context, input runtimeapi.PurgeSessionInput) (runtimeapi.PurgeSessionResult, error) {
	status, target, err := a.sessionRecordIdentity(input.Session)
	if err != nil {
		return runtimeapi.PurgeSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionPurge, status.HostEpoch, target, "", input)
	if err != nil {
		return runtimeapi.PurgeSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionPurge, protocol.SessionPurgeParams{
		SessionRecordMutation: remoteRecordMutation(attempt.id(), status, target), Guard: protocol.TrashGuard(input.Guard),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.PurgeSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionPurgeResult](value, protocol.MethodSessionPurge)
	if err != nil {
		return runtimeapi.PurgeSessionResult{}, err
	}
	a.mu.Lock()
	delete(a.sessions, target)
	a.mu.Unlock()
	return runtimeapi.PurgeSessionResult{Purged: result.Purged}, nil
}

func (a *RemoteRuntimeAdapter) SessionHistory(ctx context.Context, input runtimeapi.HistoryInput) (runtimeapi.HistoryPage, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.HistoryPage{}, err
	}
	a.mu.RLock()
	binding := a.sessions[query.Target]
	var snapshotID protocol.SnapshotID
	if binding != nil && binding.hasSnapshot {
		snapshotID = binding.snapshot.SnapshotID
	}
	a.mu.RUnlock()
	if snapshotID == "" {
		return runtimeapi.HistoryPage{}, errors.New("Remote Session has no current snapshot for history pagination")
	}
	result, err := a.client.History(ctx, protocol.SessionHistoryParams{
		RuntimeQuery: query, SnapshotID: snapshotID, Cursor: protocol.Cursor(input.Cursor), PageTurns: input.PageTurns,
	})
	if err != nil {
		return runtimeapi.HistoryPage{}, err
	}
	return mapRemoteHistory(result), nil
}

func (a *RemoteRuntimeAdapter) SessionContent(ctx context.Context, input runtimeapi.ContentInput) (runtimeapi.ContentChunk, error) {
	result, err := a.client.Content(ctx, protocol.SessionContentParams{ContentRef: protocol.ContentRef(input.ContentRef), Offset: input.Offset})
	if err != nil {
		return runtimeapi.ContentChunk{}, err
	}
	data, err := base64.StdEncoding.DecodeString(result.DataBase64)
	if err != nil {
		return runtimeapi.ContentChunk{}, fmt.Errorf("decode Remote contentRef chunk: %w", err)
	}
	return runtimeapi.ContentChunk{
		ContentRef: runtimeapi.ContentRef(result.ContentRef), Offset: result.Offset, Data: data,
		NextOffset: result.NextOffset, TotalBytes: result.TotalBytes, SHA256: result.SHA256, Encoding: string(result.Encoding),
	}, nil
}

func (a *RemoteRuntimeAdapter) ComposerSlashArgs(ctx context.Context, input runtimeapi.SlashArgsInput) (runtimeapi.SlashArgsResult, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.SlashArgsResult{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodComposerSlashArgs, protocol.ComposerSlashArgsParams{RuntimeQuery: query, Input: input.Input})
	if err != nil {
		return runtimeapi.SlashArgsResult{}, err
	}
	result, err := remoteResult[protocol.ComposerSlashArgsResult](value, protocol.MethodComposerSlashArgs)
	if err != nil {
		return runtimeapi.SlashArgsResult{}, err
	}
	items := make([]runtimeapi.SlashArgItem, len(result.Items))
	for i, item := range result.Items {
		items[i] = runtimeapi.SlashArgItem{Label: item.Label, Insert: item.Insert, Hint: item.Hint, Descend: item.Descend}
	}
	return runtimeapi.SlashArgsResult{Items: items, From: result.From}, nil
}

func (a *RemoteRuntimeAdapter) ComposerHistory(ctx context.Context, input runtimeapi.PromptHistoryInput) (runtimeapi.PromptHistoryPage, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.PromptHistoryPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodComposerHistory, protocol.ComposerHistoryParams{
		ExpectedHostEpoch: status.HostEpoch, WorkspaceID: protocol.WorkspaceID(input.WorkspaceID),
		Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.PromptHistoryPage{}, err
	}
	result, err := remoteResult[protocol.ComposerHistoryResult](value, protocol.MethodComposerHistory)
	if err != nil {
		return runtimeapi.PromptHistoryPage{}, err
	}
	entries := make([]runtimeapi.PromptHistoryEntry, len(result.Entries))
	for i, item := range result.Entries {
		entries[i] = runtimeapi.PromptHistoryEntry{Text: item.Text, AtMillis: item.AtMs, Session: mapRemoteSessionRef(item.Target), Turn: item.Turn}
	}
	return runtimeapi.PromptHistoryPage{Entries: entries, HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func (a *RemoteRuntimeAdapter) NewSession(ctx context.Context, input runtimeapi.SessionActionInput) (runtimeapi.NewSessionResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.NewSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionNew, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.NewSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionNew, protocol.SessionNewParams{SessionMutation: identity.mutation(attempt.id())})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.NewSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionNewResult](value, protocol.MethodSessionNew)
	if err != nil {
		return runtimeapi.NewSessionResult{}, err
	}
	a.recordReplacementTarget(result.Target, result.RuntimeEpoch, identity.generation)
	return runtimeapi.NewSessionResult{
		Source: mapRemoteSessionRef(result.SourceTarget), Session: mapRemoteSessionRef(result.Target),
		Disposition: result.Disposition, SnapshotRequired: result.SnapshotRequired,
	}, nil
}

func (a *RemoteRuntimeAdapter) ClearSession(ctx context.Context, input runtimeapi.SessionActionInput) (runtimeapi.ClearSessionResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.ClearSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionClear, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.ClearSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionClear, protocol.SessionClearParams{SessionMutation: identity.mutation(attempt.id())})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.ClearSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionClearResult](value, protocol.MethodSessionClear)
	if err != nil {
		return runtimeapi.ClearSessionResult{}, err
	}
	a.recordReplacementTarget(result.Target, result.RuntimeEpoch, identity.generation)
	return runtimeapi.ClearSessionResult{
		Previous: mapRemoteSessionRef(result.PreviousTarget), Session: mapRemoteSessionRef(result.Target),
		Disposition: runtimeapi.SessionClearDisposition(result.Disposition), SnapshotRequired: result.SnapshotRequired,
	}, nil
}

func (a *RemoteRuntimeAdapter) recordReplacementTarget(target protocol.RuntimeTarget, epoch protocol.RuntimeEpoch, generation uint64) {
	a.mu.Lock()
	binding := a.sessionBindingLocked(target)
	binding.runtimeEpoch = epoch
	binding.generation = generation
	binding.hasSnapshot = false
	binding.running = false
	binding.prompt = false
	a.mu.Unlock()
}

func (a *RemoteRuntimeAdapter) ForkSession(ctx context.Context, input runtimeapi.ForkSessionInput) (runtimeapi.ForkSessionResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.ForkSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionFork, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.ForkSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionFork, protocol.SessionForkParams{
		SessionMutation: identity.mutation(attempt.id()), CheckpointID: protocol.CheckpointID(input.CheckpointID), Name: input.Name,
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.ForkSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionForkResult](value, protocol.MethodSessionFork)
	if err != nil {
		return runtimeapi.ForkSessionResult{}, err
	}
	return runtimeapi.ForkSessionResult{Source: mapRemoteSessionRef(result.SourceTarget), Child: mapRemoteSessionRef(result.ChildTarget)}, nil
}

func (a *RemoteRuntimeAdapter) RewindSession(ctx context.Context, input runtimeapi.RewindSessionInput) (runtimeapi.RewindSessionResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.RewindSessionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionRewind, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.RewindSessionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionRewind, protocol.SessionRewindParams{
		SessionMutation: identity.mutation(attempt.id()), CheckpointID: protocol.CheckpointID(input.CheckpointID), Scope: protocol.RewindScope(input.Scope),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.RewindSessionResult{}, err
	}
	result, err := remoteResult[protocol.SessionRewindResult](value, protocol.MethodSessionRewind)
	if err != nil {
		return runtimeapi.RewindSessionResult{}, err
	}
	return runtimeapi.RewindSessionResult{
		WorkspaceChanged: result.WorkspaceChanged, ConversationRewritten: result.ConversationRewritten,
		SnapshotRequired: result.SnapshotRequired,
	}, nil
}

func (a *RemoteRuntimeAdapter) CompactSession(ctx context.Context, input runtimeapi.CompactSessionInput) (runtimeapi.OperationStartedResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionCompact, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionCompact, protocol.SessionCompactParams{
		SessionMutation: identity.mutation(attempt.id()), Instructions: input.Instructions,
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	result, err := remoteResult[protocol.OperationStartedResult](value, protocol.MethodSessionCompact)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	a.setOperationRunning(identity.target, result.OperationID, protocol.OperationCompact)
	return runtimeapi.OperationStartedResult{OperationID: runtimeapi.OperationID(result.OperationID), Disposition: result.Disposition}, nil
}

func (a *RemoteRuntimeAdapter) SummarizeSession(ctx context.Context, input runtimeapi.SummarizeSessionInput) (runtimeapi.OperationStartedResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionSummarize, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionSummarize, protocol.SessionSummarizeParams{
		SessionMutation: identity.mutation(attempt.id()), CheckpointID: protocol.CheckpointID(input.CheckpointID), Direction: protocol.SummaryDirection(input.Direction),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	result, err := remoteResult[protocol.OperationStartedResult](value, protocol.MethodSessionSummarize)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	a.setOperationRunning(identity.target, result.OperationID, protocol.OperationSummarize)
	return runtimeapi.OperationStartedResult{OperationID: runtimeapi.OperationID(result.OperationID), Disposition: result.Disposition}, nil
}

func (a *RemoteRuntimeAdapter) RunShell(ctx context.Context, input runtimeapi.RunShellInput) (runtimeapi.OperationStartedResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodShellRun, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodShellRun, protocol.ShellRunParams{
		SessionMutation: identity.mutation(attempt.id()), Command: input.Command,
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	result, err := remoteResult[protocol.OperationStartedResult](value, protocol.MethodShellRun)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	a.setOperationRunning(identity.target, result.OperationID, protocol.OperationShell)
	return runtimeapi.OperationStartedResult{OperationID: runtimeapi.OperationID(result.OperationID), Disposition: result.Disposition}, nil
}

func (a *RemoteRuntimeAdapter) setOperationRunning(target protocol.RuntimeTarget, id protocol.OperationID, kind protocol.OperationKind) {
	a.mu.Lock()
	binding := a.sessionBindingLocked(target)
	binding.running = true
	if binding.hasSnapshot {
		binding.snapshot.Runtime.CurrentOperation = &protocol.OperationState{OperationID: id, Kind: kind}
		binding.snapshot.Runtime.Running = true
	}
	a.mu.Unlock()
}

func (a *RemoteRuntimeAdapter) CancelOperation(ctx context.Context, input runtimeapi.CancelOperationInput) (runtimeapi.CancelOperationResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.CancelOperationResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodOperationCancel, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.CancelOperationResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodOperationCancel, protocol.OperationCancelParams{
		SessionMutation: identity.mutation(attempt.id()), ExpectedOperationID: protocol.OperationID(input.OperationID),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.CancelOperationResult{}, err
	}
	result, err := remoteResult[protocol.OperationCancelResult](value, protocol.MethodOperationCancel)
	if err != nil {
		return runtimeapi.CancelOperationResult{}, err
	}
	if result.OperationID != protocol.OperationID(input.OperationID) {
		return runtimeapi.CancelOperationResult{}, errors.New("operation/cancel response does not match requested Operation")
	}
	return runtimeapi.CancelOperationResult{Status: runtimeapi.CancelStatus(result.Status), OperationID: runtimeapi.OperationID(result.OperationID)}, nil
}

func (a *RemoteRuntimeAdapter) SetProfile(ctx context.Context, input runtimeapi.SetProfileInput) (runtimeapi.SetProfileResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.SetProfileResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionProfileSet, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.SetProfileResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionProfileSet, protocol.SessionProfileSetParams{
		SessionMutation: identity.mutation(attempt.id()), Patch: mapRemoteProfilePatch(input.Patch),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.SetProfileResult{}, err
	}
	result, err := remoteResult[protocol.SessionProfileSetResult](value, protocol.MethodSessionProfileSet)
	if err != nil {
		return runtimeapi.SetProfileResult{}, err
	}
	ids := make([]runtimeapi.PromptID, len(result.AutoResolvedPromptIDs))
	for i, id := range result.AutoResolvedPromptIDs {
		ids[i] = runtimeapi.PromptID(id)
	}
	rebuilt := result.Disposition == protocol.ProfileRebuilt
	if rebuilt {
		a.recordReplacementTarget(identity.target, result.RuntimeEpoch, identity.generation)
	}
	return runtimeapi.SetProfileResult{
		ResolvedProfile: mapRemoteResolvedProfile(result.ResolvedProfile), Disposition: runtimeapi.ProfileSetDisposition(result.Disposition),
		AutoResolvedPromptIDs: ids, SnapshotRequired: rebuilt,
	}, nil
}

func mapRemoteProfilePatch(value runtimeapi.ProfilePatch) protocol.ProfilePatch {
	result := protocol.ProfilePatch{Model: value.Model, Effort: value.Effort}
	if value.CollaborationMode != nil {
		mapped := protocol.CollaborationMode(*value.CollaborationMode)
		result.CollaborationMode = &mapped
	}
	if value.TokenMode != nil {
		mapped := protocol.TokenMode(*value.TokenMode)
		result.TokenMode = &mapped
	}
	if value.ToolApprovalMode != nil {
		mapped := protocol.ToolApprovalMode(*value.ToolApprovalMode)
		result.ToolApprovalMode = &mapped
	}
	return result
}

func (a *RemoteRuntimeAdapter) SetGoal(ctx context.Context, input runtimeapi.SetGoalInput) (runtimeapi.SetGoalResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.SetGoalResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionGoalSet, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.SetGoalResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionGoalSet, protocol.SessionGoalSetParams{SessionMutation: identity.mutation(attempt.id()), Goal: input.Goal})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.SetGoalResult{}, err
	}
	result, err := remoteResult[protocol.SessionGoalSetResult](value, protocol.MethodSessionGoalSet)
	if err != nil {
		return runtimeapi.SetGoalResult{}, err
	}
	return runtimeapi.SetGoalResult{Goal: result.Goal, Status: runtimeapi.GoalStatus(result.Status)}, nil
}

func (a *RemoteRuntimeAdapter) ResumeGoal(ctx context.Context, input runtimeapi.ResumeGoalInput) (runtimeapi.ResumeGoalResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.ResumeGoalResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionGoalResume, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.ResumeGoalResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionGoalResume, protocol.SessionGoalResumeParams{SessionMutation: identity.mutation(attempt.id())})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.ResumeGoalResult{}, err
	}
	result, err := remoteResult[protocol.SessionGoalResumeResult](value, protocol.MethodSessionGoalResume)
	if err != nil {
		return runtimeapi.ResumeGoalResult{}, err
	}
	return runtimeapi.ResumeGoalResult{Resumed: result.Resumed, Goal: result.Goal, Status: runtimeapi.GoalStatus(result.Status)}, nil
}

func (a *RemoteRuntimeAdapter) ClearGoal(ctx context.Context, input runtimeapi.ClearGoalInput) (runtimeapi.ClearGoalResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.ClearGoalResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionGoalClear, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.ClearGoalResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSessionGoalClear, protocol.SessionGoalClearParams{SessionMutation: identity.mutation(attempt.id())})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.ClearGoalResult{}, err
	}
	result, err := remoteResult[protocol.SessionGoalClearResult](value, protocol.MethodSessionGoalClear)
	if err != nil {
		return runtimeapi.ClearGoalResult{}, err
	}
	return runtimeapi.ClearGoalResult{Cleared: result.Cleared}, nil
}

func (a *RemoteRuntimeAdapter) SessionContext(ctx context.Context, input runtimeapi.SessionContextInput) (runtimeapi.ContextView, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.ContextView{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodSessionContext, protocol.SessionContextParams{RuntimeQuery: query})
	if err != nil {
		return runtimeapi.ContextView{}, err
	}
	result, err := remoteResult[protocol.SessionContextResult](value, protocol.MethodSessionContext)
	if err != nil {
		return runtimeapi.ContextView{}, err
	}
	return mapRemoteContext(result.Context), nil
}

func (a *RemoteRuntimeAdapter) SessionBalance(ctx context.Context, input runtimeapi.SessionBalanceInput) (runtimeapi.BalanceView, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.BalanceView{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodSessionBalance, protocol.SessionBalanceParams{RuntimeQuery: query})
	if err != nil {
		return runtimeapi.BalanceView{}, err
	}
	result, err := remoteResult[protocol.SessionBalanceResult](value, protocol.MethodSessionBalance)
	if err != nil {
		return runtimeapi.BalanceView{}, err
	}
	return runtimeapi.BalanceView{Available: result.Available, Display: result.Display}, nil
}

func (a *RemoteRuntimeAdapter) ListJobs(ctx context.Context, input runtimeapi.ListJobsInput) (runtimeapi.JobPage, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodJobList, protocol.JobListParams{
		RuntimeQuery: query, Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	result, err := remoteResult[protocol.JobListResult](value, protocol.MethodJobList)
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	return runtimeapi.JobPage{Jobs: mapRemoteJobs(result.Jobs), HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func (a *RemoteRuntimeAdapter) CancelJob(ctx context.Context, input runtimeapi.CancelJobInput) (runtimeapi.CancelJobResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.CancelJobResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodJobCancel, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.CancelJobResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodJobCancel, protocol.JobCancelParams{
		SessionMutation: identity.mutation(attempt.id()), JobID: protocol.JobID(input.JobID),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.CancelJobResult{}, err
	}
	result, err := remoteResult[protocol.JobCancelResult](value, protocol.MethodJobCancel)
	if err != nil {
		return runtimeapi.CancelJobResult{}, err
	}
	return runtimeapi.CancelJobResult{Disposition: runtimeapi.JobCancelDisposition(result.Disposition)}, nil
}

func (a *RemoteRuntimeAdapter) Memory(ctx context.Context, input runtimeapi.MemoryInput) (runtimeapi.MemoryView, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.MemoryView{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodMemoryGet, protocol.MemoryGetParams{RuntimeQuery: query})
	if err != nil {
		return runtimeapi.MemoryView{}, err
	}
	result, err := remoteResult[protocol.MemoryGetResult](value, protocol.MethodMemoryGet)
	if err != nil {
		return runtimeapi.MemoryView{}, err
	}
	return mapRemoteMemory(result), nil
}

func mapRemoteMemory(value protocol.MemoryGetResult) runtimeapi.MemoryView {
	result := runtimeapi.MemoryView{
		Revision: runtimeapi.CatalogRevision(value.Revision), Available: value.Available,
		Documents: make([]runtimeapi.MemoryDocument, len(value.Documents)), Facts: make([]runtimeapi.MemoryFact, len(value.Facts)),
		Archives: make([]runtimeapi.MemoryArchive, len(value.Archives)), Scopes: make([]runtimeapi.MemoryScope, len(value.Scopes)),
	}
	for i, item := range value.Documents {
		result.Documents[i] = runtimeapi.MemoryDocument{DocumentID: runtimeapi.DocumentID(item.DocumentID), Scope: item.Scope, Body: cloneStringPointer(item.Body), DisplayPath: item.DisplayPath}
	}
	for i, item := range value.Facts {
		result.Facts[i] = mapRemoteMemoryFact(item)
	}
	for i, item := range value.Archives {
		result.Archives[i] = runtimeapi.MemoryArchive{MemoryFact: mapRemoteMemoryFact(item.MemoryFact), ArchivedAt: item.ArchivedAt}
	}
	for i, item := range value.Scopes {
		result.Scopes[i] = runtimeapi.MemoryScope{Scope: item.Scope, DisplayPath: item.DisplayPath}
	}
	return result
}

func mapRemoteMemoryFact(item protocol.MemoryFact) runtimeapi.MemoryFact {
	return runtimeapi.MemoryFact{
		MemoryID: runtimeapi.MemoryID(item.MemoryID), Name: item.Name, Title: item.Title, Description: item.Description,
		Type: item.Type, Body: cloneStringPointer(item.Body),
	}
}

func (a *RemoteRuntimeAdapter) MemorySuggestions(ctx context.Context, input runtimeapi.MemoryInput) (runtimeapi.MemorySuggestionsView, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.MemorySuggestionsView{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodMemorySuggestions, protocol.MemorySuggestionsParams{RuntimeQuery: query})
	if err != nil {
		return runtimeapi.MemorySuggestionsView{}, err
	}
	result, err := remoteResult[protocol.MemorySuggestionsResult](value, protocol.MethodMemorySuggestions)
	if err != nil {
		return runtimeapi.MemorySuggestionsView{}, err
	}
	out := runtimeapi.MemorySuggestionsView{
		Revision: runtimeapi.CatalogRevision(result.Revision), Available: result.Available,
		Memories: make([]runtimeapi.MemorySuggestion, len(result.Memories)), Skills: make([]runtimeapi.SkillSuggestion, len(result.Skills)),
	}
	for i, item := range result.Memories {
		out.Memories[i] = runtimeapi.MemorySuggestion{
			SuggestionID: runtimeapi.SuggestionID(item.SuggestionID), Name: item.Name, Title: item.Title,
			Description: item.Description, Type: item.Type, Body: cloneStringPointer(item.Body), Reason: item.Reason,
			Evidence: append([]string(nil), item.Evidence...),
		}
	}
	for i, item := range result.Skills {
		out.Skills[i] = runtimeapi.SkillSuggestion{
			SuggestionID: runtimeapi.SuggestionID(item.SuggestionID), Name: item.Name, Description: item.Description,
			Scope: item.Scope, Body: cloneStringPointer(item.Body), Reason: item.Reason, Evidence: append([]string(nil), item.Evidence...),
		}
	}
	return out, nil
}

func (a *RemoteRuntimeAdapter) RememberMemory(ctx context.Context, input runtimeapi.RememberMemoryInput) (runtimeapi.RememberMemoryResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.RememberMemoryResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodMemoryRemember, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.RememberMemoryResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodMemoryRemember, protocol.MemoryRememberParams{
		SessionMutation: identity.mutation(attempt.id()), Scope: input.Scope, Note: input.Note,
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.RememberMemoryResult{}, err
	}
	result, err := remoteResult[protocol.MemoryRememberResult](value, protocol.MethodMemoryRemember)
	if err != nil {
		return runtimeapi.RememberMemoryResult{}, err
	}
	return runtimeapi.RememberMemoryResult{MemoryID: runtimeapi.MemoryID(result.MemoryID), DisplayPath: result.DisplayPath}, nil
}

func (a *RemoteRuntimeAdapter) ForgetMemory(ctx context.Context, input runtimeapi.ForgetMemoryInput) (runtimeapi.ForgetMemoryResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.ForgetMemoryResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodMemoryForget, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.ForgetMemoryResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodMemoryForget, protocol.MemoryForgetParams{
		SessionMutation: identity.mutation(attempt.id()), MemoryID: protocol.MemoryID(input.MemoryID),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.ForgetMemoryResult{}, err
	}
	result, err := remoteResult[protocol.MemoryForgetResult](value, protocol.MethodMemoryForget)
	if err != nil {
		return runtimeapi.ForgetMemoryResult{}, err
	}
	return runtimeapi.ForgetMemoryResult{Forgotten: result.Forgotten}, nil
}

func (a *RemoteRuntimeAdapter) SaveMemoryDocument(ctx context.Context, input runtimeapi.SaveMemoryDocumentInput) (runtimeapi.SaveMemoryDocumentResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.SaveMemoryDocumentResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodMemoryDocumentSave, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.SaveMemoryDocumentResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodMemoryDocumentSave, protocol.MemoryDocumentSaveParams{
		SessionMutation: identity.mutation(attempt.id()), DocumentID: protocol.DocumentID(input.DocumentID), Body: input.Body,
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.SaveMemoryDocumentResult{}, err
	}
	result, err := remoteResult[protocol.MemoryDocumentSaveResult](value, protocol.MethodMemoryDocumentSave)
	if err != nil {
		return runtimeapi.SaveMemoryDocumentResult{}, err
	}
	return runtimeapi.SaveMemoryDocumentResult{DocumentID: runtimeapi.DocumentID(result.DocumentID), Saved: result.Saved}, nil
}

func (a *RemoteRuntimeAdapter) AcceptMemorySuggestion(ctx context.Context, input runtimeapi.AcceptMemorySuggestionInput) (runtimeapi.AcceptMemorySuggestionResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.AcceptMemorySuggestionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodMemorySuggestionAccept, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.AcceptMemorySuggestionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodMemorySuggestionAccept, protocol.MemorySuggestionAcceptParams{
		SessionMutation: identity.mutation(attempt.id()), SuggestionID: protocol.SuggestionID(input.SuggestionID), ExpectedRevision: protocol.CatalogRevision(input.ExpectedRevision),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.AcceptMemorySuggestionResult{}, err
	}
	result, err := remoteResult[protocol.MemorySuggestionAcceptResult](value, protocol.MethodMemorySuggestionAccept)
	if err != nil {
		return runtimeapi.AcceptMemorySuggestionResult{}, err
	}
	return runtimeapi.AcceptMemorySuggestionResult{MemoryID: runtimeapi.MemoryID(result.MemoryID)}, nil
}

func (a *RemoteRuntimeAdapter) AcceptSkillSuggestion(ctx context.Context, input runtimeapi.AcceptSkillSuggestionInput) (runtimeapi.AcceptSkillSuggestionResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.AcceptSkillSuggestionResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSkillSuggestionAccept, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.AcceptSkillSuggestionResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodSkillSuggestionAccept, protocol.SkillSuggestionAcceptParams{
		SessionMutation: identity.mutation(attempt.id()), SuggestionID: protocol.SuggestionID(input.SuggestionID), ExpectedRevision: protocol.CatalogRevision(input.ExpectedRevision),
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.AcceptSkillSuggestionResult{}, err
	}
	result, err := remoteResult[protocol.SkillSuggestionAcceptResult](value, protocol.MethodSkillSuggestionAccept)
	if err != nil {
		return runtimeapi.AcceptSkillSuggestionResult{}, err
	}
	return runtimeapi.AcceptSkillSuggestionResult{SkillID: runtimeapi.SkillID(result.SkillID)}, nil
}

func (a *RemoteRuntimeAdapter) ResearchStatus(ctx context.Context, input runtimeapi.ResearchInput) (runtimeapi.ResearchStatusView, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.ResearchStatusView{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodResearchStatus, protocol.ResearchStatusParams{RuntimeQuery: query})
	if err != nil {
		return runtimeapi.ResearchStatusView{}, err
	}
	result, err := remoteResult[protocol.ResearchStatusResult](value, protocol.MethodResearchStatus)
	if err != nil {
		return runtimeapi.ResearchStatusView{}, err
	}
	view := runtimeapi.ResearchStatusView{Available: result.Available}
	if result.Task != nil {
		mapped := mapRemoteResearchTask(*result.Task)
		view.Task = &mapped
	}
	return view, nil
}

func (a *RemoteRuntimeAdapter) ListResearch(ctx context.Context, input runtimeapi.ListResearchInput) (runtimeapi.ResearchPage, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodResearchList, protocol.ResearchListParams{
		RuntimeQuery: query, Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	result, err := remoteResult[protocol.ResearchListResult](value, protocol.MethodResearchList)
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	items := make([]runtimeapi.ResearchTask, len(result.Items))
	for i, item := range result.Items {
		items[i] = mapRemoteResearchTask(item)
	}
	return runtimeapi.ResearchPage{Items: items, HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func mapRemoteResearchTask(item protocol.ResearchTask) runtimeapi.ResearchTask {
	criteria := make([]runtimeapi.ResearchCriterion, len(item.OpenCriteria))
	for i, criterion := range item.OpenCriteria {
		criteria[i] = runtimeapi.ResearchCriterion{
			CriterionID: runtimeapi.CriterionID(criterion.CriterionID), Description: criterion.Description,
			Required: criterion.Required, EvidenceCount: criterion.EvidenceCount, Status: criterion.Status,
		}
	}
	return runtimeapi.ResearchTask{
		TaskID: runtimeapi.ResearchTaskID(item.TaskID), Goal: cloneStringPointer(item.Goal), Status: item.Status,
		Iteration: item.Iteration, CurrentDirection: cloneStringPointer(item.CurrentDirection), StaleCount: item.StaleCount,
		PivotCount: item.PivotCount, PivotRequired: item.PivotRequired, LastHeartbeatAt: item.LastHeartbeatAt,
		FindingCount: item.FindingCount, OpenCriteria: criteria, Blocker: cloneStringPointer(item.Blocker),
		DisplayPath: item.DisplayPath, NextRequiredAction: cloneStringPointer(item.NextRequiredAction),
	}
}

func (a *RemoteRuntimeAdapter) ResearchFindings(ctx context.Context, input runtimeapi.ResearchFindingsInput) (runtimeapi.ResearchFindingsPage, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodResearchFindings, protocol.ResearchFindingsParams{
		RuntimeQuery: query, TaskID: protocol.ResearchTaskID(input.TaskID), Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	result, err := remoteResult[protocol.ResearchFindingsResult](value, protocol.MethodResearchFindings)
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	items := make([]runtimeapi.ResearchFinding, len(result.Items))
	for i, item := range result.Items {
		items[i] = runtimeapi.ResearchFinding{
			ID: item.ID, Kind: item.Kind, Summary: cloneStringPointer(item.Summary), Source: item.Source, Command: item.Command,
			Paths: append([]string(nil), item.Paths...), Accepted: item.Accepted, CreatedAt: item.CreatedAt,
		}
	}
	return runtimeapi.ResearchFindingsPage{Items: items, HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func (a *RemoteRuntimeAdapter) RecordResearchEvidence(ctx context.Context, input runtimeapi.RecordResearchEvidenceInput) (runtimeapi.RecordResearchEvidenceResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodResearchEvidenceRecord, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, err
	}
	value, requestErr := a.client.Request(ctx, protocol.MethodResearchEvidenceRecord, protocol.ResearchEvidenceRecordParams{
		SessionMutation: identity.mutation(attempt.id()), TaskID: protocol.ResearchTaskID(input.TaskID), CriterionID: protocol.CriterionID(input.CriterionID),
		Evidence: protocol.ResearchEvidence{
			ID: input.Evidence.ID, Kind: input.Evidence.Kind, Summary: input.Evidence.Summary, Source: input.Evidence.Source,
			Command: input.Evidence.Command, Paths: append([]string(nil), input.Evidence.Paths...), Accepted: input.Evidence.Accepted,
		},
	})
	if err = attempt.finish(requestErr); err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, err
	}
	result, err := remoteResult[protocol.ResearchEvidenceRecordResult](value, protocol.MethodResearchEvidenceRecord)
	if err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, err
	}
	return runtimeapi.RecordResearchEvidenceResult{Recorded: result.Recorded}, nil
}

func (a *RemoteRuntimeAdapter) ListFiles(ctx context.Context, input runtimeapi.FileListInput) (runtimeapi.FileListResult, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.FileListResult{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodFileList, protocol.FileListParams{
		RuntimeQuery: query, Path: input.Path, Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.FileListResult{}, err
	}
	result, err := remoteResult[protocol.FileListResult](value, protocol.MethodFileList)
	if err != nil {
		return runtimeapi.FileListResult{}, err
	}
	entries := make([]runtimeapi.FileEntry, len(result.Entries))
	for i, item := range result.Entries {
		entries[i] = runtimeapi.FileEntry{Name: item.Name, Path: item.Path, IsDir: item.IsDir}
	}
	return runtimeapi.FileListResult{Entries: entries, HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor)}, nil
}

func (a *RemoteRuntimeAdapter) SearchFiles(ctx context.Context, input runtimeapi.FileSearchInput) (runtimeapi.FileSearchResult, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.FileSearchResult{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodFileSearch, protocol.FileSearchParams{
		RuntimeQuery: query, Query: input.Query, Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.FileSearchResult{}, err
	}
	result, err := remoteResult[protocol.FileSearchResult](value, protocol.MethodFileSearch)
	if err != nil {
		return runtimeapi.FileSearchResult{}, err
	}
	entries := make([]runtimeapi.FileEntry, len(result.Entries))
	for i, item := range result.Entries {
		entries[i] = runtimeapi.FileEntry{Name: item.Name, Path: item.Path, IsDir: item.IsDir}
	}
	return runtimeapi.FileSearchResult{
		Entries: entries, Truncated: result.Truncated, TruncationReason: runtimeapi.SearchTruncationReason(result.TruncationReason),
		ReturnedItems: result.ReturnedItems, TotalItems: result.TotalItems,
	}, nil
}

func (a *RemoteRuntimeAdapter) PreviewFile(ctx context.Context, input runtimeapi.FilePreviewInput) (runtimeapi.FilePreview, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.FilePreview{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodFilePreview, protocol.FilePreviewParams{RuntimeQuery: query, Path: input.Path})
	if err != nil {
		return runtimeapi.FilePreview{}, err
	}
	result, err := remoteResult[protocol.FilePreviewResult](value, protocol.MethodFilePreview)
	if err != nil {
		return runtimeapi.FilePreview{}, err
	}
	out := runtimeapi.FilePreview{
		Name: result.Name, Path: result.Path, Kind: runtimeapi.FileKind(result.Kind), SizeBytes: result.SizeBytes,
		ReturnedBytes: result.ReturnedBytes, Binary: result.Binary, Truncated: result.Truncated,
		TruncationReason: runtimeapi.ByteTruncationReason(result.TruncationReason), Body: cloneStringPointer(result.Body),
	}
	if err := out.Validate(); err != nil {
		return runtimeapi.FilePreview{}, fmt.Errorf("file/preview invariant: %w", err)
	}
	return out, nil
}

func (a *RemoteRuntimeAdapter) WorkspaceChanges(ctx context.Context, input runtimeapi.WorkspaceChangesInput) (runtimeapi.WorkspaceChangesPage, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.WorkspaceChangesPage{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodWorkspaceChanges, protocol.WorkspaceChangesParams{
		RuntimeQuery: query, Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.WorkspaceChangesPage{}, err
	}
	result, err := remoteResult[protocol.WorkspaceChangesResult](value, protocol.MethodWorkspaceChanges)
	if err != nil {
		return runtimeapi.WorkspaceChangesPage{}, err
	}
	files := make([]runtimeapi.ChangedFile, len(result.Files))
	for i, item := range result.Files {
		sources := make([]runtimeapi.ChangeSource, len(item.Sources))
		for j, source := range item.Sources {
			sources[j] = runtimeapi.ChangeSource(source)
		}
		files[i] = runtimeapi.ChangedFile{
			Path: item.Path, OldPath: item.OldPath, Sources: sources, GitStatus: item.GitStatus,
			Turns: append([]int(nil), item.Turns...), LatestPrompt: item.LatestPrompt, LatestTimeMillis: cloneInt64Pointer(item.LatestTimeMs),
		}
	}
	return runtimeapi.WorkspaceChangesPage{
		Files: files, GitAvailable: result.GitAvailable, GitBranch: result.GitBranch,
		HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor),
	}, nil
}

func (a *RemoteRuntimeAdapter) GitHistory(ctx context.Context, input runtimeapi.GitHistoryInput) (runtimeapi.GitHistoryResult, error) {
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.GitHistoryResult{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodGitHistory, protocol.GitHistoryParams{RuntimeQuery: query, Path: input.Path})
	if err != nil {
		return runtimeapi.GitHistoryResult{}, err
	}
	result, err := remoteResult[protocol.GitHistoryResult](value, protocol.MethodGitHistory)
	if err != nil {
		return runtimeapi.GitHistoryResult{}, err
	}
	commits := make([]runtimeapi.GitCommit, len(result.Commits))
	for i, item := range result.Commits {
		commits[i] = runtimeapi.GitCommit{Hash: item.Hash, Author: item.Author, Date: item.Date, Message: item.Message}
	}
	return runtimeapi.GitHistoryResult{
		Commits: commits, Truncated: result.Truncated,
		TruncationReason: runtimeapi.GitHistoryTruncationReason(result.TruncationReason), ReturnedItems: result.ReturnedItems,
	}, nil
}

func (a *RemoteRuntimeAdapter) GitCommitDetail(ctx context.Context, input runtimeapi.GitCommitDetailInput) (runtimeapi.GitCommitDetail, error) {
	if err := input.Validate(); err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	query, err := a.runtimeQuery(input.Session)
	if err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodGitCommitDetail, protocol.GitCommitDetailParams{
		RuntimeQuery: query, Hash: input.Hash, Path: input.Path, Cursor: protocol.Cursor(input.Cursor), Limit: remoteLimit(input.Limit),
	})
	if err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	result, err := remoteResult[protocol.GitCommitDetailResult](value, protocol.MethodGitCommitDetail)
	if err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	out := runtimeapi.GitCommitDetail{
		Kind: runtimeapi.GitCommitDetailKind(result.Kind), HasMore: result.HasMore, Next: runtimeapi.Cursor(result.NextCursor),
		Path: result.Path, Body: cloneStringPointer(result.Body), SizeBytes: cloneInt64Pointer(result.SizeBytes),
		ReturnedBytes: cloneInt64Pointer(result.ReturnedBytes), Truncated: cloneBoolPointer(result.Truncated),
		TruncationReason: runtimeapi.ByteTruncationReason(result.TruncationReason),
	}
	if result.Files != nil {
		files := make([]runtimeapi.GitCommitFile, len(*result.Files))
		for i, item := range *result.Files {
			files[i] = runtimeapi.GitCommitFile{Path: item.Path, OldPath: item.OldPath, Status: item.Status, Additions: item.Additions, Deletions: item.Deletions}
		}
		out.Files = &files
	}
	if err := out.Validate(); err != nil {
		return runtimeapi.GitCommitDetail{}, fmt.Errorf("git/commitDetail invariant: %w", err)
	}
	return out, nil
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
