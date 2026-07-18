package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"reasonix/internal/runtimeapi"
)

const remoteCatalogMaxPages = 100

// attachRemoteWorkbenchSession validates an opaque SessionRef against the Host
// catalogs, performs an atomic attach+subscribe, and only then publishes or
// refreshes its Desktop tab binding.
func (a *App) attachRemoteWorkbenchSession(ref runtimeapi.SessionRef, activate bool) (TabMeta, error) {
	if !ref.Valid() {
		return TabMeta{}, errors.New("a valid Remote Session identity is required")
	}
	api, target, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return TabMeta{}, err
	}
	manager := a.remote.manager
	if manager == nil {
		return TabMeta{}, ErrRuntimeTargetUnavailable
	}
	before := manager.Snapshot()
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	workspace, err := remoteWorkspaceByID(ctx, api, ref.WorkspaceID)
	if err != nil {
		return TabMeta{}, err
	}
	summary, err := remoteSessionByRef(ctx, api, ref)
	if err != nil {
		return TabMeta{}, err
	}
	snapshot, err := api.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: ref, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
	})
	if err != nil {
		return TabMeta{}, fmt.Errorf("attach Remote Session: %w", err)
	}
	if snapshot.Session != ref {
		return TabMeta{}, errors.New("Remote attach returned a different Session identity")
	}
	after := manager.Snapshot()
	if after.State != TargetRemoteConnected || after.Generation != before.Generation || !sameTarget(after.Target, before.Target) || after.Target.ID != target.ID {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	created := runtimeapi.CreatedSession{
		Session: ref, TopicID: summary.TopicID, TopicTitle: summary.Title, ResolvedProfile: snapshot.Profile,
	}
	tabID := a.commitRemoteWorkbenchSession(target.ID, workspace, created, snapshot, after.Generation, activate)
	meta, ok := a.remoteTabMeta(tabID)
	if !ok {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	a.publishRemoteWorkbenchReady(a.RemoteWorkbenchStatus())
	return meta, nil
}

func (a *App) commitRemoteWorkbenchSession(
	hostID string,
	workspace runtimeapi.Workspace,
	created runtimeapi.CreatedSession,
	snapshot runtimeapi.SessionSnapshot,
	generation uint64,
	activate bool,
) string {
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	state := &a.remote.workbench
	if state.HostID != hostID {
		*state = remoteWorkbenchState{HostID: hostID}
	}
	state.ensureMaps()
	state.Workspaces[workspace.ID] = workspace
	tabID := remoteSessionTabID(created.Session)
	if current := state.Sessions[created.Session]; current != nil {
		current.Created = created
		current.Snapshot = cloneRemoteSessionSnapshot(snapshot)
		current.HistoryCursors = remoteHistoryCursorsFromSnapshot(snapshot)
		current.AttachedGeneration = generation
		current.ReattachGeneration = 0
		current.LastAttachError = ""
	} else {
		state.Sessions[created.Session] = &remoteWorkbenchSession{
			Created: created, Snapshot: cloneRemoteSessionSnapshot(snapshot), HistoryCursors: remoteHistoryCursorsFromSnapshot(snapshot),
			AttachedGeneration: generation,
		}
		state.TabOrder = append(state.TabOrder, tabID)
	}
	state.SessionTabs[tabID] = created.Session
	if activate || state.ActiveTabID == "" {
		state.ActiveTabID = tabID
	}
	return tabID
}

func (a *App) remoteOpenProjectTab(workspaceID, topicID string) (TabMeta, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	topicID = strings.TrimSpace(topicID)
	if workspaceID == "" {
		return TabMeta{}, errors.New("Remote workspace ID is required")
	}
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return TabMeta{}, err
	}
	workspace := runtimeapi.WorkspaceID(workspaceID)
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	sessions, err := listAllRemoteSessions(ctx, api, workspace)
	if err != nil {
		return TabMeta{}, err
	}
	var selected *runtimeapi.SessionSummary
	for index := range sessions {
		candidate := &sessions[index]
		if topicID != "" && candidate.TopicID != runtimeapi.TopicID(topicID) {
			continue
		}
		if selected == nil || candidate.LastActivityMillis > selected.LastActivityMillis {
			selected = candidate
		}
	}
	if selected != nil {
		return a.attachRemoteWorkbenchSession(selected.Session, true)
	}
	selection := runtimeapi.TopicSelection{Kind: runtimeapi.TopicNew, Title: defaultTopicTitle}
	if topicID != "" {
		selection = runtimeapi.TopicSelection{Kind: runtimeapi.TopicExisting, TopicID: runtimeapi.TopicID(topicID)}
	}
	created, err := api.CreateSession(ctx, runtimeapi.CreateSessionInput{WorkspaceID: workspace, Topic: selection})
	if err != nil {
		return TabMeta{}, err
	}
	return a.attachRemoteWorkbenchCreatedSession(api, created, true)
}

func (a *App) remoteOpenTopicSession(workspaceID, topicID, sessionID string) (TabMeta, error) {
	ref := runtimeapi.SessionRef{
		WorkspaceID: runtimeapi.WorkspaceID(strings.TrimSpace(workspaceID)),
		SessionID:   runtimeapi.SessionID(strings.TrimSpace(sessionID)),
	}
	if !ref.Valid() {
		return TabMeta{}, errors.New("Remote workspace and Session identities are required")
	}
	if topicID != "" {
		api, _, err := a.remoteConnectedV1Runtime()
		if err != nil {
			return TabMeta{}, err
		}
		ctx, cancel := a.remoteActionContext()
		summary, err := remoteSessionByRef(ctx, api, ref)
		cancel()
		if err != nil {
			return TabMeta{}, err
		}
		if summary.TopicID != runtimeapi.TopicID(strings.TrimSpace(topicID)) {
			return TabMeta{}, errors.New("Remote Session does not belong to the selected topic")
		}
	}
	return a.attachRemoteWorkbenchSession(ref, true)
}

func (a *App) attachRemoteWorkbenchCreatedSession(api runtimeapi.V1RuntimeAPI, created runtimeapi.CreatedSession, activate bool) (TabMeta, error) {
	if api == nil || !created.Session.Valid() {
		return TabMeta{}, errors.New("a valid created Remote Session is required")
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	workspace, err := remoteWorkspaceByID(ctx, api, created.Session.WorkspaceID)
	if err != nil {
		return TabMeta{}, err
	}
	manager := a.remote.manager
	if manager == nil {
		return TabMeta{}, ErrRuntimeTargetUnavailable
	}
	before := manager.Snapshot()
	snapshot, err := api.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: created.Session, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
	})
	if err != nil {
		return TabMeta{}, err
	}
	if snapshot.Session != created.Session {
		return TabMeta{}, errors.New("Remote attach returned a different Session identity")
	}
	after := manager.Snapshot()
	if before.State != TargetRemoteConnected || after.State != TargetRemoteConnected || before.Generation != after.Generation || !sameTarget(before.Target, after.Target) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	created.TopicID = snapshot.TopicID
	created.TopicTitle = snapshot.Title
	created.ResolvedProfile = snapshot.Profile
	tabID := a.commitRemoteWorkbenchSession(after.Target.ID, workspace, created, snapshot, after.Generation, activate)
	meta, ok := a.remoteTabMeta(tabID)
	if !ok {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	a.publishRemoteWorkbenchReady(a.RemoteWorkbenchStatus())
	return meta, nil
}

func (a *App) remoteEnsureBlankTab(workspaceID string) (TabMeta, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		a.remote.workbenchMu.RLock()
		if session, _, ok := a.remote.workbench.activeSession(); ok {
			workspaceID = string(session.Created.Session.WorkspaceID)
		}
		a.remote.workbenchMu.RUnlock()
	}
	if workspaceID == "" {
		return TabMeta{}, errors.New("Remote workspace ID is required")
	}
	a.remote.workbenchMu.RLock()
	for _, tabID := range a.remote.workbench.TabOrder {
		ref := a.remote.workbench.SessionTabs[tabID]
		session := a.remote.workbench.Sessions[ref]
		if session != nil && ref.WorkspaceID == runtimeapi.WorkspaceID(workspaceID) &&
			session.Snapshot.History.TotalTurns == 0 && !session.Snapshot.Runtime.Running && session.Snapshot.PendingPrompt == nil {
			a.remote.workbenchMu.RUnlock()
			if err := a.remoteSetActiveTab(tabID); err != nil {
				return TabMeta{}, err
			}
			meta, _ := a.remoteTabMeta(tabID)
			return meta, nil
		}
	}
	a.remote.workbenchMu.RUnlock()
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return TabMeta{}, err
	}
	ctx, cancel := a.remoteActionContext()
	created, err := api.CreateSession(ctx, runtimeapi.CreateSessionInput{
		WorkspaceID: runtimeapi.WorkspaceID(workspaceID),
		Topic:       runtimeapi.TopicSelection{Kind: runtimeapi.TopicNew, Title: defaultTopicTitle},
	})
	cancel()
	if err != nil {
		return TabMeta{}, err
	}
	return a.attachRemoteWorkbenchCreatedSession(api, created, true)
}

func (a *App) remoteKeepOnlyVisibleTab(tabID string) (TabMeta, error) {
	if err := a.remoteSetActiveTab(tabID); err != nil {
		return TabMeta{}, err
	}
	a.remote.workbenchMu.RLock()
	order := append([]string(nil), a.remote.workbench.TabOrder...)
	a.remote.workbenchMu.RUnlock()
	for _, other := range order {
		if other == tabID {
			continue
		}
		if err := a.closeRemoteWorkbenchSession(other); err != nil {
			return TabMeta{}, err
		}
	}
	meta, ok := a.remoteTabMeta(tabID)
	if !ok {
		return TabMeta{}, fmt.Errorf("Remote tab %q not found", tabID)
	}
	return meta, nil
}

func (a *App) remoteSetActiveTab(tabID string) error {
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	state := &a.remote.workbench
	if state.HostID == "" {
		return ErrRuntimeTargetUnavailable
	}
	if _, ok := state.SessionTabs[tabID]; !ok {
		return fmt.Errorf("Remote tab %q not found", tabID)
	}
	state.ActiveTabID = tabID
	return nil
}

func (a *App) remoteReorderTabs(tabIDs []string) error {
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	state := &a.remote.workbench
	if len(tabIDs) != len(state.TabOrder) {
		return errors.New("tab order length mismatch")
	}
	seen := make(map[string]bool, len(tabIDs))
	for _, tabID := range tabIDs {
		if _, ok := state.SessionTabs[tabID]; !ok {
			return fmt.Errorf("Remote tab %q not found", tabID)
		}
		if seen[tabID] {
			return fmt.Errorf("duplicate Remote tab %q", tabID)
		}
		seen[tabID] = true
	}
	state.TabOrder = append([]string(nil), tabIDs...)
	return nil
}

func (a *App) remoteCreateTopic(workspaceID, title string) (TopicMeta, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return TopicMeta{}, errors.New("Remote workspace ID is required")
	}
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return TopicMeta{}, err
	}
	ctx, cancel := a.remoteActionContext()
	created, err := api.CreateTopic(ctx, runtimeapi.CreateTopicInput{WorkspaceID: runtimeapi.WorkspaceID(workspaceID), Title: strings.TrimSpace(title)})
	cancel()
	if err != nil {
		return TopicMeta{}, err
	}
	a.emitProjectTreeChanged()
	return TopicMeta{ID: string(created.TopicID), Title: created.Title, CreatedAt: created.CreatedAtMillis}, nil
}

func (a *App) remoteRenameTopic(topicID, title string) error {
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return err
	}
	lookupCtx, lookupCancel := a.remoteActionContext()
	workspaceID, err := a.remoteWorkspaceForTopic(lookupCtx, api, runtimeapi.TopicID(strings.TrimSpace(topicID)))
	lookupCancel()
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.RenameTopic(ctx, runtimeapi.RenameTopicInput{
		WorkspaceID: workspaceID, TopicID: runtimeapi.TopicID(strings.TrimSpace(topicID)), Title: strings.TrimSpace(title),
	})
	cancel()
	if err != nil {
		return err
	}
	a.remote.workbenchMu.Lock()
	for ref, session := range a.remote.workbench.Sessions {
		if session != nil && ref.WorkspaceID == workspaceID && session.Created.TopicID == runtimeapi.TopicID(topicID) {
			session.Created.TopicTitle = result.Title
			session.Snapshot.Title = result.Title
		}
	}
	a.remote.workbenchMu.Unlock()
	a.publishRemoteWorkbenchChanged()
	return nil
}

func (a *App) remoteDeleteTopic(topicID string) error {
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return err
	}
	id := runtimeapi.TopicID(strings.TrimSpace(topicID))
	lookupCtx, lookupCancel := a.remoteActionContext()
	workspaceID, err := a.remoteWorkspaceForTopic(lookupCtx, api, id)
	lookupCancel()
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.DeleteTopic(ctx, runtimeapi.DeleteTopicInput{WorkspaceID: workspaceID, TopicID: id})
	cancel()
	if err == nil {
		a.emitProjectTreeChanged()
	}
	return err
}

func (a *App) remoteTrashTopic(topicID string) error {
	a.remote.workbenchOp.Lock()
	defer a.remote.workbenchOp.Unlock()

	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return err
	}
	id := runtimeapi.TopicID(strings.TrimSpace(topicID))
	lookupCtx, lookupCancel := a.remoteActionContext()
	workspaceID, err := a.remoteWorkspaceForTopic(lookupCtx, api, id)
	lookupCancel()
	if err != nil {
		return err
	}
	target := a.remote.manager.Snapshot()
	if target.State != TargetRemoteConnected {
		return ErrRuntimeTargetUnavailable
	}
	a.remote.workbenchMu.RLock()
	matching := make([]runtimeapi.SessionRef, 0)
	for _, tabID := range a.remote.workbench.TabOrder {
		ref, exists := a.remote.workbench.SessionTabs[tabID]
		if !exists {
			continue
		}
		session := a.remote.workbench.Sessions[ref]
		if session == nil || ref.WorkspaceID != workspaceID || session.Created.TopicID != id {
			continue
		}
		if session.Snapshot.Runtime.Running || session.Snapshot.Runtime.CurrentTurn != nil ||
			session.Snapshot.Runtime.CurrentOperation != nil || session.Snapshot.PendingPrompt != nil || len(session.Snapshot.Jobs) != 0 {
			a.remote.workbenchMu.RUnlock()
			return errors.New("Remote topic has active work; cancel its Turn, prompt, Operation, and jobs before moving it to trash")
		}
		matching = append(matching, ref)
	}
	a.remote.workbenchMu.RUnlock()

	unsubscribed := make([]runtimeapi.SessionRef, 0, len(matching))
	for _, ref := range matching {
		ctx, cancel := a.remoteActionContext()
		unsubscribeErr := api.UnsubscribeSession(ctx, runtimeapi.UnsubscribeSessionInput{Session: ref})
		cancel()
		if unsubscribeErr != nil {
			rollbackErr := a.restoreRemoteTopicSubscriptions(api, unsubscribed, target.Generation)
			if rollbackErr != nil {
				return fmt.Errorf("unsubscribe Remote topic before trash: %w (subscription rollback failed: %v)", unsubscribeErr, rollbackErr)
			}
			return fmt.Errorf("unsubscribe Remote topic before trash: %w", unsubscribeErr)
		}
		unsubscribed = append(unsubscribed, ref)
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.TrashTopic(ctx, runtimeapi.TrashTopicInput{WorkspaceID: workspaceID, TopicID: id})
	cancel()
	if err != nil {
		rollbackErr := a.restoreRemoteTopicSubscriptions(api, unsubscribed, target.Generation)
		if rollbackErr != nil {
			return fmt.Errorf("trash Remote topic: %w (subscription rollback failed: %v)", err, rollbackErr)
		}
		return err
	}
	a.remote.workbenchMu.Lock()
	state := &a.remote.workbench
	for ref, session := range state.Sessions {
		if session == nil || ref.WorkspaceID != workspaceID || session.Created.TopicID != id {
			continue
		}
		tabID := remoteSessionTabID(ref)
		delete(state.Sessions, ref)
		delete(state.SessionTabs, tabID)
		state.TabOrder = removeRemoteTabID(state.TabOrder, tabID)
	}
	if _, ok := state.SessionTabs[state.ActiveTabID]; !ok {
		state.ActiveTabID = ""
		if len(state.TabOrder) != 0 {
			state.ActiveTabID = state.TabOrder[0]
		}
	}
	a.remote.workbenchMu.Unlock()
	a.publishRemoteWorkbenchChanged()
	return nil
}

func (a *App) restoreRemoteTopicSubscriptions(api runtimeapi.V1RuntimeAPI, refs []runtimeapi.SessionRef, generation uint64) error {
	var restoreErrors []error
	for _, ref := range refs {
		ctx, cancel := a.remoteActionContext()
		snapshot, err := api.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
			Session: ref, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
		})
		cancel()
		if err == nil && snapshot.Session != ref {
			err = errors.New("Remote rollback attach returned a different Session identity")
		}
		currentTarget := a.remote.manager.Snapshot()
		if err == nil && (currentTarget.State != TargetRemoteConnected || currentTarget.Generation != generation) {
			err = ErrTargetTransitionSuperseded
		}
		if err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("%s: %w", ref.SessionID, err))
			continue
		}
		a.remote.workbenchMu.Lock()
		if session := a.remote.workbench.Sessions[ref]; session != nil && session.AttachedGeneration == generation {
			session.Snapshot = cloneRemoteSessionSnapshot(snapshot)
			session.HistoryCursors = remoteHistoryCursorsFromSnapshot(snapshot)
			session.Created.TopicID = snapshot.TopicID
			session.Created.TopicTitle = snapshot.Title
			session.Created.ResolvedProfile = snapshot.Profile
			session.LastAttachError = ""
		}
		a.remote.workbenchMu.Unlock()
	}
	return errors.Join(restoreErrors...)
}

func (a *App) remoteWorkspaceForTopic(ctx context.Context, api runtimeapi.V1RuntimeAPI, topicID runtimeapi.TopicID) (runtimeapi.WorkspaceID, error) {
	if topicID == "" {
		return "", errors.New("Remote topic ID is required")
	}
	a.remote.workbenchMu.RLock()
	for ref, session := range a.remote.workbench.Sessions {
		if session != nil && session.Created.TopicID == topicID {
			a.remote.workbenchMu.RUnlock()
			return ref.WorkspaceID, nil
		}
	}
	a.remote.workbenchMu.RUnlock()
	workspaces, err := listAllRemoteWorkspaces(ctx, api)
	if err != nil {
		return "", err
	}
	for _, workspace := range workspaces {
		topics, listErr := listAllRemoteTopics(ctx, api, workspace.ID)
		if listErr != nil {
			return "", listErr
		}
		for _, topic := range topics {
			if topic.TopicID == topicID {
				return workspace.ID, nil
			}
		}
	}
	return "", fmt.Errorf("Remote topic %q not found", topicID)
}

func (a *App) remoteProjectTree() []ProjectNode {
	api, target, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return []ProjectNode{}
	}
	layout := newRemoteLayoutDocument(target.ID)
	if persisted, layoutErr := loadRemoteLayout(a.remote.store, target.ID); layoutErr != nil {
		slog.Warn("desktop: load Remote project layout failed", "host", target.ID, "err", layoutErr)
	} else {
		layout = persisted
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	workspaces, err := listAllRemoteWorkspaces(ctx, api)
	if err != nil {
		slog.Warn("desktop: list Remote workspaces for project tree failed", "err", err)
		return []ProjectNode{}
	}
	a.remote.workbenchMu.Lock()
	a.remote.workbench.ensureMaps()
	for _, workspace := range workspaces {
		a.remote.workbench.Workspaces[workspace.ID] = workspace
	}
	a.remote.workbenchMu.Unlock()
	out := make([]ProjectNode, 0, len(workspaces))
	for _, workspace := range workspaces {
		topics, topicErr := listAllRemoteTopics(ctx, api, workspace.ID)
		sessions, sessionErr := listAllRemoteSessions(ctx, api, workspace.ID)
		if topicErr != nil || sessionErr != nil {
			slog.Warn("desktop: list Remote project catalog failed", "workspace", workspace.ID, "topicsErr", topicErr, "sessionsErr", sessionErr)
			continue
		}
		topicsByID := make(map[runtimeapi.TopicID]runtimeapi.TopicSummary, len(topics))
		for _, topic := range topics {
			topicsByID[topic.TopicID] = topic
		}
		for _, session := range sessions {
			if _, ok := topicsByID[session.TopicID]; !ok {
				topicsByID[session.TopicID] = runtimeapi.TopicSummary{
					TopicID: session.TopicID, Title: session.Title, CreatedAtMillis: session.CreatedAtMillis,
					LastActivityMillis: session.LastActivityMillis,
				}
			}
		}
		orderedTopics := make([]runtimeapi.TopicSummary, 0, len(topicsByID))
		for _, topic := range topicsByID {
			orderedTopics = append(orderedTopics, topic)
		}
		sort.SliceStable(orderedTopics, func(i, j int) bool {
			return orderedTopics[i].LastActivityMillis > orderedTopics[j].LastActivityMillis
		})
		children := make([]ProjectNode, 0, len(orderedTopics))
		for _, topic := range orderedTopics {
			sessionNodes := make([]ProjectNode, 0, topic.SessionCount)
			turns := 0
			open := false
			running := false
			for _, summary := range sessions {
				if summary.TopicID != topic.TopicID {
					continue
				}
				isOpen, isRunning, status := a.remoteSessionCatalogState(summary)
				open = open || isOpen
				running = running || isRunning
				if summary.Turns > turns {
					turns = summary.Turns
				}
				sessionNodes = append(sessionNodes, ProjectNode{
					Key: "remote_session_" + remoteSessionTabID(summary.Session), Kind: "session",
					Label: summary.Title, Root: string(workspace.ID), TopicID: string(summary.TopicID),
					SessionPath: string(summary.Session.SessionID), Turns: summary.Turns,
					CreatedAt: summary.CreatedAtMillis, LastActivityAt: summary.LastActivityMillis,
					Open: isOpen, Running: isRunning, Status: status, Recovered: summary.RecoveryInterrupted,
				})
			}
			children = append(children, ProjectNode{
				Key: "remote_topic_" + string(topic.TopicID), Kind: "topic", Label: topic.Title,
				Root: string(workspace.ID), TopicID: string(topic.TopicID), Turns: turns,
				CreatedAt: topic.CreatedAtMillis, LastActivityAt: topic.LastActivityMillis,
				Open: open, Running: running, Children: sessionNodes,
			})
		}
		out = append(out, ProjectNode{
			Key: "remote_workspace_" + string(workspace.ID), Kind: "project", Label: workspace.Name,
			Root: string(workspace.ID), Children: children,
		})
	}
	return applyRemoteProjectLayout(out, layout)
}

func (a *App) remoteSessionCatalogState(summary runtimeapi.SessionSummary) (bool, bool, string) {
	a.remote.workbenchMu.RLock()
	defer a.remote.workbenchMu.RUnlock()
	session := a.remote.workbench.Sessions[summary.Session]
	if session == nil {
		running := summary.Runtime != nil && (summary.Runtime.Running || summary.Runtime.PendingPrompt || summary.Runtime.ActiveJobs > 0)
		return false, running, remoteRuntimeStatus(summary.Runtime)
	}
	running := session.Snapshot.Runtime.Running || session.Snapshot.PendingPrompt != nil || len(session.Snapshot.Jobs) > 0
	return true, running, remoteSnapshotStatus(session.Snapshot)
}

func remoteRuntimeStatus(summary *runtimeapi.SessionRuntimeSummary) string {
	if summary == nil {
		return ""
	}
	if summary.PendingPrompt {
		return topicStatusWaitingConfirmation
	}
	if summary.Running {
		return topicStatusStreaming
	}
	if summary.ActiveJobs > 0 {
		return topicStatusBackgroundJob
	}
	return ""
}

func remoteSnapshotStatus(snapshot runtimeapi.SessionSnapshot) string {
	if snapshot.PendingPrompt != nil {
		return topicStatusWaitingConfirmation
	}
	if snapshot.Runtime.Running {
		return topicStatusStreaming
	}
	if len(snapshot.Jobs) > 0 {
		return topicStatusBackgroundJob
	}
	return ""
}

func remoteWorkspaceByID(ctx context.Context, api runtimeapi.V1RuntimeAPI, workspaceID runtimeapi.WorkspaceID) (runtimeapi.Workspace, error) {
	workspaces, err := listAllRemoteWorkspaces(ctx, api)
	if err != nil {
		return runtimeapi.Workspace{}, err
	}
	for _, workspace := range workspaces {
		if workspace.ID == workspaceID {
			return workspace, nil
		}
	}
	return runtimeapi.Workspace{}, fmt.Errorf("Remote workspace %q not found", workspaceID)
}

func remoteSessionByRef(ctx context.Context, api runtimeapi.V1RuntimeAPI, ref runtimeapi.SessionRef) (runtimeapi.SessionSummary, error) {
	sessions, err := listAllRemoteSessions(ctx, api, ref.WorkspaceID)
	if err != nil {
		return runtimeapi.SessionSummary{}, err
	}
	for _, session := range sessions {
		if session.Session == ref {
			return session, nil
		}
	}
	return runtimeapi.SessionSummary{}, fmt.Errorf("Remote Session %q not found", ref.SessionID)
}

func listAllRemoteWorkspaces(ctx context.Context, api runtimeapi.V1RuntimeAPI) ([]runtimeapi.Workspace, error) {
	var out []runtimeapi.Workspace
	var cursor runtimeapi.Cursor
	seen := map[runtimeapi.Cursor]bool{"": true}
	for page := 0; page < remoteCatalogMaxPages; page++ {
		result, err := api.ListWorkspaces(ctx, runtimeapi.ListWorkspacesInput{Cursor: cursor, Limit: runtimeapi.PageMaxItems})
		if err != nil {
			return nil, err
		}
		out = append(out, result.Items...)
		next, more, err := advanceRemoteCatalogCursor("workspace", cursor, result.Next, result.HasMore, seen)
		if err != nil {
			return nil, err
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
	return nil, errors.New("Remote workspace catalog exceeded its page bound")
}

func listAllRemoteTopics(ctx context.Context, api runtimeapi.V1RuntimeAPI, workspaceID runtimeapi.WorkspaceID) ([]runtimeapi.TopicSummary, error) {
	var out []runtimeapi.TopicSummary
	var cursor runtimeapi.Cursor
	seen := map[runtimeapi.Cursor]bool{"": true}
	for page := 0; page < remoteCatalogMaxPages; page++ {
		result, err := api.ListTopics(ctx, runtimeapi.ListTopicsInput{WorkspaceID: workspaceID, Cursor: cursor, Limit: runtimeapi.PageMaxItems})
		if err != nil {
			return nil, err
		}
		out = append(out, result.Items...)
		next, more, err := advanceRemoteCatalogCursor("topic", cursor, result.Next, result.HasMore, seen)
		if err != nil {
			return nil, err
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
	return nil, errors.New("Remote topic catalog exceeded its page bound")
}

func listAllRemoteSessions(ctx context.Context, api runtimeapi.V1RuntimeAPI, workspaceID runtimeapi.WorkspaceID) ([]runtimeapi.SessionSummary, error) {
	var out []runtimeapi.SessionSummary
	var cursor runtimeapi.Cursor
	seen := map[runtimeapi.Cursor]bool{"": true}
	for page := 0; page < remoteCatalogMaxPages; page++ {
		result, err := api.ListSessions(ctx, runtimeapi.ListSessionsInput{WorkspaceID: workspaceID, Cursor: cursor, Limit: runtimeapi.PageMaxItems})
		if err != nil {
			return nil, err
		}
		out = append(out, result.Items...)
		next, more, err := advanceRemoteCatalogCursor("Session", cursor, result.Next, result.HasMore, seen)
		if err != nil {
			return nil, err
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
	return nil, errors.New("Remote Session catalog exceeded its page bound")
}

func listAllRemoteTrash(ctx context.Context, api runtimeapi.V1RuntimeAPI, workspaceID runtimeapi.WorkspaceID) ([]runtimeapi.TrashEntry, error) {
	var out []runtimeapi.TrashEntry
	var cursor runtimeapi.Cursor
	seen := map[runtimeapi.Cursor]bool{"": true}
	for page := 0; page < remoteCatalogMaxPages; page++ {
		result, err := api.ListTrashedSessions(ctx, runtimeapi.ListTrashedSessionsInput{WorkspaceID: workspaceID, Cursor: cursor, Limit: runtimeapi.PageMaxItems})
		if err != nil {
			return nil, err
		}
		out = append(out, result.Items...)
		next, more, err := advanceRemoteCatalogCursor("trash", cursor, result.Next, result.HasMore, seen)
		if err != nil {
			return nil, err
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
	return nil, errors.New("Remote trash catalog exceeded its page bound")
}

func advanceRemoteCatalogCursor(
	domain string,
	current runtimeapi.Cursor,
	next runtimeapi.Cursor,
	hasMore bool,
	seen map[runtimeapi.Cursor]bool,
) (runtimeapi.Cursor, bool, error) {
	if !hasMore {
		if next != "" {
			return "", false, fmt.Errorf("Remote %s catalog returned a cursor without hasMore", domain)
		}
		return "", false, nil
	}
	if next == "" || next == current || seen[next] {
		return "", false, fmt.Errorf("Remote %s catalog returned an invalid cursor", domain)
	}
	seen[next] = true
	return next, true, nil
}
