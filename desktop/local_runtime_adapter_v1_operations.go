package main

import (
	"context"
	"errors"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
	"reasonix/internal/skill"
)

func localComposerSymbols(ctrl control.SessionAPI) runtimeservice.ComposerSymbols {
	result := runtimeservice.ComposerSymbols{}
	for _, command := range ctrl.Commands() {
		if !command.Hidden {
			result.TurnCommands = append(result.TurnCommands, command.Name)
		}
	}
	if host := ctrl.Host(); host != nil {
		for _, prompt := range host.Prompts() {
			result.TurnCommands = append(result.TurnCommands, prompt.Name)
		}
	}
	for _, invocation := range ctrl.SlashSkills() {
		kind := runtimeapi.InvocationSkill
		if invocation.RunAs == skill.RunSubagent {
			kind = runtimeapi.InvocationSubagent
		}
		result.Invocations = append(result.Invocations, runtimeservice.ComposerInvocationSymbol{Name: invocation.SlashName(), Kind: kind})
	}
	return result
}

func (a *LocalTargetAdapter) ComposerSubmit(ctx context.Context, input runtimeapi.ComposerSubmitInput) (runtimeapi.ComposerSubmitResult, error) {
	tab, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.ComposerSubmitResult{}, err
	}
	route, err := runtimeservice.RouteComposerSubmit(input, localComposerSymbols(ctrl))
	a.endLocalSession()
	if err != nil {
		return runtimeapi.ComposerSubmitResult{}, err
	}

	switch route.Kind {
	case runtimeservice.ComposerTurn:
		return a.localComposerTurn(ctx, tab.ID, input.Session, route)
	case runtimeservice.ComposerOperation:
		var started runtimeapi.OperationStartedResult
		switch route.Operation {
		case runtimeapi.OperationShell:
			started, err = a.RunShell(ctx, runtimeapi.RunShellInput{Session: input.Session, Command: route.Argument})
		case runtimeapi.OperationCompact:
			started, err = a.CompactSession(ctx, runtimeapi.CompactSessionInput{Session: input.Session, Instructions: route.Argument})
		default:
			err = errors.New("unsupported Local composer operation")
		}
		if err != nil {
			return runtimeapi.ComposerSubmitResult{}, err
		}
		return runtimeapi.ComposerSubmitResult{Kind: runtimeapi.SubmitOperation, OperationID: started.OperationID, Operation: string(route.Operation), Session: input.Session}, nil
	case runtimeservice.ComposerLifecycle:
		return a.localComposerLifecycle(ctx, tab.ID, input.Session, route)
	case runtimeservice.ComposerCompleted:
		return a.localComposerCompletion(ctx, tab.ID, input.Session, route)
	default:
		return runtimeapi.ComposerSubmitResult{}, errors.New("unsupported Local composer route")
	}
}

func (a *LocalTargetAdapter) localComposerTurn(ctx context.Context, tabID string, ref runtimeapi.SessionRef, route runtimeservice.ComposerRoute) (runtimeapi.ComposerSubmitResult, error) {
	turnID, err := newLocalOpaqueID("local_turn")
	if err != nil {
		return runtimeapi.ComposerSubmitResult{}, err
	}
	a.mu.Lock()
	state, _, err := a.sessionLocked(ref)
	if err == nil {
		if state.currentTurn != "" || state.currentOperation != nil || state.pendingPrompt != nil {
			err = control.ErrTurnRunning
		} else {
			state.currentTurn = runtimeapi.TurnID(turnID)
			state.cancelRequested = false
			state.liveEvents = nil
		}
	}
	a.mu.Unlock()
	if err != nil {
		return runtimeapi.ComposerSubmitResult{}, err
	}
	if err := localCheckContext(ctx); err != nil {
		a.clearTurn(ref, runtimeapi.TurnID(turnID))
		return runtimeapi.ComposerSubmitResult{}, err
	}

	switch route.Turn {
	case runtimeservice.ComposerTurnNormal, runtimeservice.ComposerTurnRawSlash:
		if route.EditedOriginal != "" {
			err = a.app.SubmitEditedDisplayToTab(tabID, route.DisplayText, route.Input, route.EditedOriginal)
		} else {
			err = a.app.SubmitDisplayToTab(tabID, route.DisplayText, route.Input)
		}
	case runtimeservice.ComposerTurnEdited:
		err = a.app.SubmitEditedDisplayToTab(tabID, route.DisplayText, route.Input, route.EditedOriginal)
	case runtimeservice.ComposerTurnInvocations:
		requests := make([]InvocationRequest, len(route.Invocations))
		for index, invocation := range route.Invocations {
			requests[index] = InvocationRequest{Name: invocation.Name, Kind: string(invocation.Kind), Offset: index}
		}
		err = a.app.SubmitInvocationsToTab(tabID, route.DisplayText, route.Input, requests)
	case runtimeservice.ComposerTurnRecovery:
		err = a.app.SubmitDeliveryRecoveryToTab(tabID, route.DisplayText, route.Input)
	default:
		err = errors.New("unsupported Local composer Turn primitive")
	}
	if err != nil {
		a.clearTurn(ref, runtimeapi.TurnID(turnID))
		return runtimeapi.ComposerSubmitResult{}, err
	}
	return runtimeapi.ComposerSubmitResult{Kind: runtimeapi.SubmitTurn, TurnID: runtimeapi.TurnID(turnID), Session: ref}, nil
}

func (a *LocalTargetAdapter) localComposerLifecycle(ctx context.Context, tabID string, ref runtimeapi.SessionRef, route runtimeservice.ComposerRoute) (runtimeapi.ComposerSubmitResult, error) {
	result := runtimeapi.ComposerSubmitResult{Kind: runtimeapi.SubmitCompleted, Effect: string(route.Effect), Session: ref, SnapshotRequired: true}
	var err error
	switch route.Lifecycle {
	case runtimeservice.ComposerLifecycleNew:
		value, callErr := a.NewSession(ctx, runtimeapi.SessionActionInput{Session: ref})
		err, result.Session = callErr, value.Session
	case runtimeservice.ComposerLifecycleClear:
		value, callErr := a.ClearSession(ctx, runtimeapi.SessionActionInput{Session: ref})
		err, result.Session = callErr, value.Session
	case runtimeservice.ComposerLifecycleBranch, runtimeservice.ComposerLifecycleRewind:
		// The existing Local controller owns raw /branch and /rewind argument
		// grammar. Reusing that exact dispatcher preserves native behavior while
		// RouteComposerSubmit still owns the shared result union.
		err = a.app.SubmitDisplayToTab(tabID, route.DisplayText, route.Input)
		if err == nil {
			a.mu.Lock()
			a.refreshSessionsLocked()
			if next := a.tabSessions[tabID]; next.Valid() {
				result.Session = next
			}
			a.mu.Unlock()
		}
	default:
		err = errors.New("unsupported Local composer lifecycle")
	}
	if err != nil {
		return runtimeapi.ComposerSubmitResult{}, err
	}
	return result, nil
}

func (a *LocalTargetAdapter) localComposerCompletion(ctx context.Context, tabID string, ref runtimeapi.SessionRef, route runtimeservice.ComposerRoute) (runtimeapi.ComposerSubmitResult, error) {
	if route.Completion == runtimeservice.ComposerCompletionMemoryRemember {
		if _, err := a.RememberMemory(ctx, runtimeapi.RememberMemoryInput{Session: ref, Scope: "project", Note: route.Argument}); err != nil {
			return runtimeapi.ComposerSubmitResult{}, err
		}
	} else if route.Completion == runtimeservice.ComposerCompletionProfileEffort {
		// Keep the existing Local command grammar authoritative: an empty
		// argument displays the current effort while a value changes it.
		if err := a.app.SubmitToTab(tabID, route.Input); err != nil {
			return runtimeapi.ComposerSubmitResult{}, err
		}
	} else {
		// Host-management writes are denied only over Remote. Local retains its
		// established Desktop dispatcher and therefore can execute /mcp, /skill,
		// and other machine-local management commands normally.
		if err := a.app.SubmitDisplayToTab(tabID, route.DisplayText, route.Input); err != nil {
			return runtimeapi.ComposerSubmitResult{}, err
		}
	}
	return runtimeapi.ComposerSubmitResult{Kind: runtimeapi.SubmitCompleted, Effect: string(route.Effect), Session: ref}, nil
}

func (a *LocalTargetAdapter) NewSession(ctx context.Context, input runtimeapi.SessionActionInput) (runtimeapi.NewSessionResult, error) {
	tab, _, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.NewSessionResult{}, err
	}
	defer a.endLocalSession()
	if err := a.app.NewSessionForTab(tab.ID); err != nil {
		return runtimeapi.NewSessionResult{}, err
	}
	a.mu.Lock()
	a.refreshSessionsLocked()
	next := a.tabSessions[tab.ID]
	if next != input.Session {
		a.invalidateLocalCheckpointsLocked(input.Session)
	}
	a.mu.Unlock()
	if !next.Valid() {
		return runtimeapi.NewSessionResult{}, ErrLocalSessionUnknown
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogSessions, runtimeapi.CatalogTopics, runtimeapi.CatalogSessionCatalog)
	return runtimeapi.NewSessionResult{Source: input.Session, Session: next, Disposition: "created", SnapshotRequired: true}, nil
}

func (a *LocalTargetAdapter) ClearSession(ctx context.Context, input runtimeapi.SessionActionInput) (runtimeapi.ClearSessionResult, error) {
	tab, _, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.ClearSessionResult{}, err
	}
	defer a.endLocalSession()
	if err := a.app.ClearSessionForTab(tab.ID); err != nil {
		return runtimeapi.ClearSessionResult{}, err
	}
	a.mu.Lock()
	a.refreshSessionsLocked()
	next := a.tabSessions[tab.ID]
	a.invalidateLocalCheckpointsLocked(input.Session)
	a.mu.Unlock()
	if !next.Valid() {
		return runtimeapi.ClearSessionResult{}, ErrLocalSessionUnknown
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogSessions, runtimeapi.CatalogSessionCatalog)
	return runtimeapi.ClearSessionResult{Previous: input.Session, Session: next, Disposition: runtimeapi.SessionCleared, SnapshotRequired: true}, nil
}

func (a *LocalTargetAdapter) checkpointTurn(tabID string, ref runtimeapi.SessionRef, checkpointID runtimeapi.CheckpointID) (int, error) {
	if strings.TrimSpace(string(checkpointID)) == "" {
		return 0, errors.New("checkpointId is required")
	}
	values := a.app.CheckpointsForTab(tabID)
	a.mu.Lock()
	_, err := a.syncLocalCheckpointsLocked(ref, values)
	target, ok := a.v1.checkpointIDs[checkpointID]
	a.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if !ok || target.session != ref {
		return 0, errors.New("Local checkpoint ID is stale or unknown")
	}
	for _, checkpoint := range values {
		if checkpoint.Turn == target.turn && localCheckpointFingerprint(checkpoint) == target.fingerprint {
			return checkpoint.Turn, nil
		}
	}
	return 0, errors.New("Local checkpoint ID is stale or unknown")
}

func (a *LocalTargetAdapter) ForkSession(ctx context.Context, input runtimeapi.ForkSessionInput) (runtimeapi.ForkSessionResult, error) {
	tab, _, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.ForkSessionResult{}, err
	}
	turn, err := a.checkpointTurn(tab.ID, input.Session, input.CheckpointID)
	if err != nil {
		a.endLocalSession()
		return runtimeapi.ForkSessionResult{}, err
	}
	defer a.endLocalSession()
	meta, err := a.app.ForkForTab(tab.ID, turn)
	if err != nil {
		return runtimeapi.ForkSessionResult{}, err
	}
	if strings.TrimSpace(input.Name) != "" && strings.TrimSpace(meta.SessionPath) != "" {
		if err := agent.RenameSession(meta.SessionPath, input.Name); err != nil {
			return runtimeapi.ForkSessionResult{}, err
		}
	}
	a.mu.Lock()
	a.refreshSessionsLocked()
	child := a.tabSessions[meta.ID]
	a.mu.Unlock()
	if !child.Valid() {
		return runtimeapi.ForkSessionResult{}, ErrLocalSessionUnknown
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogSessions, runtimeapi.CatalogTopics)
	return runtimeapi.ForkSessionResult{Source: input.Session, Child: child}, nil
}

func (a *LocalTargetAdapter) RewindSession(ctx context.Context, input runtimeapi.RewindSessionInput) (runtimeapi.RewindSessionResult, error) {
	tab, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.RewindSessionResult{}, err
	}
	turn, err := a.checkpointTurn(tab.ID, input.Session, input.CheckpointID)
	if err != nil {
		a.endLocalSession()
		return runtimeapi.RewindSessionResult{}, err
	}
	var scope control.RewindScope
	switch input.Scope {
	case runtimeapi.RewindCode:
		scope = control.RewindCode
	case runtimeapi.RewindConversation:
		scope = control.RewindConversation
	case runtimeapi.RewindBoth:
		scope = control.RewindBoth
	default:
		a.endLocalSession()
		return runtimeapi.RewindSessionResult{}, errors.New("invalid rewind scope")
	}
	result, err := ctrl.RewindDetailed(turn, scope)
	a.endLocalSession()
	if err != nil {
		projected := runtimeapi.RewindSessionResult{SnapshotRequired: result.SnapshotRequired}
		var rewindErr *control.RewindError
		if errors.As(err, &rewindErr) {
			projected.WorkspaceChanged = rewindErr.WorkspaceMayHaveChanged
			projected.ConversationRewritten = rewindErr.ConversationMayHaveChanged
			projected.SnapshotRequired = rewindErr.SnapshotRequired
			if rewindErr.ConversationMayHaveChanged {
				a.mu.Lock()
				a.invalidateLocalCheckpointsLocked(input.Session)
				a.mu.Unlock()
			}
		}
		return projected, err
	}
	if scope == control.RewindConversation || scope == control.RewindBoth {
		a.mu.Lock()
		a.invalidateLocalCheckpointsLocked(input.Session)
		a.mu.Unlock()
	}
	return runtimeapi.RewindSessionResult{
		WorkspaceChanged: result.WorkspaceChanged, ConversationRewritten: result.ConversationRewritten,
		SnapshotRequired: result.SnapshotRequired,
	}, nil
}

func (a *LocalTargetAdapter) startLocalOperation(ctx context.Context, ref runtimeapi.SessionRef, kind runtimeapi.OperationKind, spec control.OperationSpec) (runtimeapi.OperationStartedResult, error) {
	tab, ctrl, _, err := a.withLocalSession(ctx, ref)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	defer a.endLocalSession()
	operationID, err := newLocalOpaqueID("local_operation")
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	a.mu.Lock()
	state, _, err := a.sessionLocked(ref)
	if err == nil && (state.currentTurn != "" || state.currentOperation != nil || state.pendingPrompt != nil) {
		err = control.ErrSessionBusy
	}
	if err == nil {
		var handle *control.OperationHandle
		handle, err = ctrl.StartOperation(spec)
		if err == nil {
			state.currentOperation = &localRuntimeOperation{id: runtimeapi.OperationID(operationID), kind: kind, handle: handle}
			state.cancelRequested = false
			state.liveEvents = nil
		}
	}
	a.mu.Unlock()
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	a.app.ensureTabTopicIndexedForUserTurn(tab)
	return runtimeapi.OperationStartedResult{OperationID: runtimeapi.OperationID(operationID), Disposition: "started"}, nil
}

func (a *LocalTargetAdapter) CompactSession(ctx context.Context, input runtimeapi.CompactSessionInput) (runtimeapi.OperationStartedResult, error) {
	return a.startLocalOperation(ctx, input.Session, runtimeapi.OperationCompact, control.OperationSpec{Kind: control.OperationCompact, Instructions: input.Instructions})
}

func (a *LocalTargetAdapter) SummarizeSession(ctx context.Context, input runtimeapi.SummarizeSessionInput) (runtimeapi.OperationStartedResult, error) {
	tab, _, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	turn, err := a.checkpointTurn(tab.ID, input.Session, input.CheckpointID)
	a.endLocalSession()
	if err != nil {
		return runtimeapi.OperationStartedResult{}, err
	}
	direction := control.SummarizeDirection(input.Direction)
	return a.startLocalOperation(ctx, input.Session, runtimeapi.OperationSummarize, control.OperationSpec{Kind: control.OperationSummarize, Turn: turn, Direction: direction})
}

func (a *LocalTargetAdapter) RunShell(ctx context.Context, input runtimeapi.RunShellInput) (runtimeapi.OperationStartedResult, error) {
	return a.startLocalOperation(ctx, input.Session, runtimeapi.OperationShell, control.OperationSpec{Kind: control.OperationShell, Command: input.Command})
}

func (a *LocalTargetAdapter) CancelOperation(ctx context.Context, input runtimeapi.CancelOperationInput) (runtimeapi.CancelOperationResult, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.CancelOperationResult{}, err
	}
	a.mu.Lock()
	state, _, err := a.sessionLocked(input.Session)
	if err != nil {
		a.mu.Unlock()
		return runtimeapi.CancelOperationResult{}, err
	}
	operation := state.currentOperation
	if operation == nil || operation.id != input.OperationID {
		a.mu.Unlock()
		return runtimeapi.CancelOperationResult{}, errors.New("Local operation ID is not active for this session")
	}
	attempt := operation.handle.Cancel()
	if attempt == control.OperationCancelRequestedNow || attempt == control.OperationCancelAlreadyRequested {
		state.cancelRequested = true
	}
	a.mu.Unlock()
	switch attempt {
	case control.OperationCancelRequestedNow:
		return runtimeapi.CancelOperationResult{Status: runtimeapi.CancelRequested, OperationID: input.OperationID}, nil
	case control.OperationCancelAlreadyRequested:
		return runtimeapi.CancelOperationResult{Status: runtimeapi.CancelAlreadyRequested, OperationID: input.OperationID}, nil
	default:
		return runtimeapi.CancelOperationResult{}, errors.New("Local operation is no longer cancellable")
	}
}

func (a *LocalTargetAdapter) SetProfile(ctx context.Context, input runtimeapi.SetProfileInput) (runtimeapi.SetProfileResult, error) {
	tab, _, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.SetProfileResult{}, err
	}
	defer a.endLocalSession()
	rebuilt := false
	if input.Patch.Model != nil {
		if err := a.app.SetModelForTab(tab.ID, *input.Patch.Model); err != nil {
			return runtimeapi.SetProfileResult{}, err
		}
		rebuilt = true
	}
	if input.Patch.Effort != nil {
		if err := a.app.SetEffortForTab(tab.ID, *input.Patch.Effort); err != nil {
			return runtimeapi.SetProfileResult{}, err
		}
		rebuilt = true
	}
	if input.Patch.CollaborationMode != nil {
		a.app.SetCollaborationModeForTab(tab.ID, *input.Patch.CollaborationMode)
	}
	if input.Patch.TokenMode != nil {
		if err := a.app.SetTokenModeForTab(tab.ID, *input.Patch.TokenMode); err != nil {
			return runtimeapi.SetProfileResult{}, err
		}
		rebuilt = true
	}
	autoResolved := []runtimeapi.PromptID{}
	if input.Patch.ToolApprovalMode != nil {
		rawIDs := a.app.SetToolApprovalModeForTab(tab.ID, *input.Patch.ToolApprovalMode)
		a.mu.Lock()
		for _, raw := range rawIDs {
			if id := a.rawPrompts[tab.ID+"\x00"+raw]; id != "" {
				autoResolved = append(autoResolved, id)
			}
		}
		a.mu.Unlock()
	}
	a.app.mu.RLock()
	current := a.app.tabs[tab.ID]
	resolved := runtimeapi.ResolvedProfile{}
	if current != nil {
		resolved = runtimeapi.ResolvedProfile{Model: current.model, CollaborationMode: currentTabCollaborationMode(current), TokenMode: currentTabTokenMode(current), ToolApprovalMode: currentTabToolApprovalMode(current)}
		if current.effort != nil {
			resolved.Effort = *current.effort
		}
	}
	a.app.mu.RUnlock()
	disposition := runtimeapi.ProfileUpdated
	if rebuilt {
		disposition = runtimeapi.ProfileRebuilt
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogSessionCatalog, runtimeapi.CatalogSessions)
	return runtimeapi.SetProfileResult{ResolvedProfile: resolved, Disposition: disposition, AutoResolvedPromptIDs: autoResolved, SnapshotRequired: rebuilt}, nil
}

func (a *LocalTargetAdapter) SetGoal(ctx context.Context, input runtimeapi.SetGoalInput) (runtimeapi.SetGoalResult, error) {
	tab, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.SetGoalResult{}, err
	}
	defer a.endLocalSession()
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return runtimeapi.SetGoalResult{}, errors.New("goal is required")
	}
	a.app.SetGoalForTab(tab.ID, goal)
	return runtimeapi.SetGoalResult{Goal: ctrl.Goal(), Status: runtimeapi.GoalStatus(ctrl.GoalStatus())}, nil
}

func (a *LocalTargetAdapter) ResumeGoal(ctx context.Context, input runtimeapi.ResumeGoalInput) (runtimeapi.ResumeGoalResult, error) {
	tab, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.ResumeGoalResult{}, err
	}
	defer a.endLocalSession()
	resumed := a.app.ResumeGoalForTab(tab.ID)
	return runtimeapi.ResumeGoalResult{Resumed: resumed, Goal: ctrl.Goal(), Status: runtimeapi.GoalStatus(ctrl.GoalStatus())}, nil
}

func (a *LocalTargetAdapter) ClearGoal(ctx context.Context, input runtimeapi.ClearGoalInput) (runtimeapi.ClearGoalResult, error) {
	tab, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.ClearGoalResult{}, err
	}
	defer a.endLocalSession()
	had := strings.TrimSpace(ctrl.Goal()) != ""
	a.app.ClearGoalForTab(tab.ID)
	return runtimeapi.ClearGoalResult{Cleared: had}, nil
}
