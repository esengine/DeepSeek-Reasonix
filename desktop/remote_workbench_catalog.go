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
	a.remote.workbenchOp.Lock()
	defer a.remote.workbenchOp.Unlock()

	if !ref.Valid() {
		return TabMeta{}, errors.New("a valid Remote Session identity is required")
	}
	api, expected, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return TabMeta{}, err
	}
	return a.attachRemoteWorkbenchSessionForTarget(api, ref, activate, expected)
}

// attachRemoteWorkbenchSessionForTarget keeps the catalog lookup, atomic
// attach, and workbench commit bound to the exact target generation that chose
// ref. Opaque Session identities are Host-scoped and must never be retried
// against whichever Host happens to be current later.
func (a *App) attachRemoteWorkbenchSessionForTarget(
	api runtimeapi.V1RuntimeAPI,
	ref runtimeapi.SessionRef,
	activate bool,
	expected TargetManagerSnapshot,
) (TabMeta, error) {
	if api == nil || !ref.Valid() {
		return TabMeta{}, errors.New("a valid Remote Session identity is required")
	}
	manager := a.remote.manager
	if manager == nil {
		return TabMeta{}, ErrRuntimeTargetUnavailable
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	workspace, err := remoteWorkspaceByID(ctx, api, ref.WorkspaceID)
	if err != nil {
		return TabMeta{}, err
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	summary, err := remoteSessionByRef(ctx, api, ref)
	if err != nil {
		return TabMeta{}, err
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	snapshot, rollback, err := attachRemoteWorkbenchAtomic(ctx, api, runtimeapi.AttachAndSubscribeInput{
		Session: ref, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
	})
	if err != nil {
		return TabMeta{}, fmt.Errorf("attach Remote Session: %w", err)
	}
	retainSubscription := false
	defer func() {
		if !retainSubscription {
			discardRemoteWorkbenchSubscription(rollback, ref)
		}
	}()
	if snapshot.Session != ref {
		return TabMeta{}, errors.New("Remote attach returned a different Session identity")
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	created := runtimeapi.CreatedSession{
		Session: ref, TopicID: summary.TopicID, TopicTitle: summary.Title, ResolvedProfile: snapshot.Profile,
	}
	tabID, _, committed := a.commitRemoteWorkbenchSession(expected, workspace, created, snapshot, activate)
	if !committed {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	retainSubscription = true
	meta, ok := a.remoteTabMeta(tabID)
	if !ok {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	return meta, nil
}

func (a *App) commitRemoteWorkbenchSession(
	expected TargetManagerSnapshot,
	workspace runtimeapi.Workspace,
	created runtimeapi.CreatedSession,
	snapshot runtimeapi.SessionSnapshot,
	activate bool,
) (string, RemoteWorkbenchStatusView, bool) {
	tabID := remoteSessionTabID(created.Session)
	var status RemoteWorkbenchStatusView
	if a == nil || a.remote.manager == nil {
		return tabID, status, false
	}
	manager := a.remote.manager
	manager.stateDispatchMu.Lock()
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		manager.stateDispatchMu.Unlock()
		return tabID, status, false
	}
	a.remote.workbenchMu.Lock()
	state := &a.remote.workbench
	if state.HostID != expected.Target.ID {
		*state = remoteWorkbenchState{HostID: expected.Target.ID}
	}
	state.ensureMaps()
	state.Workspaces[workspace.ID] = workspace
	if current := state.Sessions[created.Session]; current != nil {
		current.Created = created
		current.Snapshot = cloneRemoteSessionSnapshot(snapshot)
		current.HistoryCursors = remoteHistoryCursorsFromSnapshot(snapshot)
		current.AttachedGeneration = expected.Generation
		current.ReattachGeneration = 0
		current.LastAttachError = ""
	} else {
		state.Sessions[created.Session] = &remoteWorkbenchSession{
			Created: created, Snapshot: cloneRemoteSessionSnapshot(snapshot), HistoryCursors: remoteHistoryCursorsFromSnapshot(snapshot),
			AttachedGeneration: expected.Generation,
		}
		state.TabOrder = append(state.TabOrder, tabID)
	}
	state.SessionTabs[tabID] = created.Session
	if activate || state.ActiveTabID == "" {
		state.ActiveTabID = tabID
	}
	for fingerprint, pending := range state.Pending {
		if pending != nil && pending.HostID == expected.Target.ID && pending.Created.Session == created.Session {
			delete(state.Pending, fingerprint)
		}
	}
	if byFingerprint := a.remote.workspacePending[expected.Target.ID]; byFingerprint != nil {
		for fingerprint, pending := range byFingerprint {
			if pending != nil && pending.Created.Session == created.Session {
				delete(byFingerprint, fingerprint)
			}
		}
		if len(byFingerprint) == 0 {
			delete(a.remote.workspacePending, expected.Target.ID)
		}
	}
	status = remoteWorkbenchStatusLocked(*state, expected)
	a.remote.workbenchMu.Unlock()
	// The state commit and all target-sensitive notifications share the same
	// dispatch critical section as StateSink. Once this sequence is queued, a
	// newer target generation may publish, but it can never be overtaken by it.
	a.enqueueRemoteWorkbenchReady(status)
	manager.stateDispatchMu.Unlock()
	a.emitProjectTreeChanged()
	return tabID, status, true
}

func (a *App) remoteOpenProjectTab(workspaceID, topicID string) (TabMeta, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	topicID = strings.TrimSpace(topicID)
	if workspaceID == "" {
		return TabMeta{}, errors.New("Remote workspace ID is required")
	}
	finishCreate, err := a.beginRemoteWorkbenchSessionCreate()
	if err != nil {
		return TabMeta{}, err
	}
	defer finishCreate()

	api, expected, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return TabMeta{}, err
	}
	manager := a.remote.manager
	if manager == nil {
		return TabMeta{}, ErrRuntimeTargetUnavailable
	}
	workspace := runtimeapi.WorkspaceID(workspaceID)
	fingerprint := remoteProjectSessionCreateFingerprint(workspace, runtimeapi.TopicID(topicID))
	if pending := a.remoteWorkspacePending(expected.Target.ID, fingerprint); pending != nil {
		return a.attachRemoteWorkbenchSessionForTarget(api, pending.Created.Session, true, expected)
	}
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
		return a.attachRemoteWorkbenchSessionForTarget(api, selected.Session, true, expected)
	}
	selection := runtimeapi.TopicSelection{Kind: runtimeapi.TopicNew, Title: defaultTopicTitle}
	if topicID != "" {
		selection = runtimeapi.TopicSelection{Kind: runtimeapi.TopicExisting, TopicID: runtimeapi.TopicID(topicID)}
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	created, err := api.CreateSession(ctx, runtimeapi.CreateSessionInput{WorkspaceID: workspace, Topic: selection})
	if err != nil {
		return TabMeta{}, err
	}
	if !created.Session.Valid() || created.Session.WorkspaceID != workspace {
		return TabMeta{}, errors.New("Remote create returned a Session in a different workspace")
	}
	a.storeRemoteWorkspacePending(&pendingRemoteSessionCreate{
		HostID: expected.Target.ID, Fingerprint: fingerprint,
		Workspace: runtimeapi.Workspace{ID: workspace}, Created: created,
	})
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	return a.attachRemoteWorkbenchCreatedSession(api, created, true, expected)
}

func (a *App) remoteOpenTopicSession(workspaceID, topicID, sessionID string) (TabMeta, error) {
	a.remote.workbenchOp.Lock()
	defer a.remote.workbenchOp.Unlock()

	workspace := runtimeapi.WorkspaceID(strings.TrimSpace(workspaceID))
	ref, encoded, err := parseRemoteSessionToken(sessionID)
	if err != nil {
		return TabMeta{}, err
	}
	if encoded {
		if ref.WorkspaceID != workspace {
			return TabMeta{}, errors.New("Remote Session token does not belong to the selected workspace")
		}
	} else {
		// Raw IDs remain accepted for older Desktop callers. New projections use
		// remoteSessionToken so the complete opaque SessionRef survives every
		// history/tree/tab round trip.
		ref = runtimeapi.SessionRef{WorkspaceID: workspace, SessionID: runtimeapi.SessionID(strings.TrimSpace(sessionID))}
	}
	if !ref.Valid() {
		return TabMeta{}, errors.New("Remote workspace and Session identities are required")
	}
	api, expected, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return TabMeta{}, err
	}
	if topicID != "" {
		ctx, cancel := a.remoteActionContext()
		summary, err := remoteSessionByRef(ctx, api, ref)
		cancel()
		if err != nil {
			return TabMeta{}, err
		}
		if summary.TopicID != runtimeapi.TopicID(strings.TrimSpace(topicID)) {
			return TabMeta{}, errors.New("Remote Session does not belong to the selected topic")
		}
		return a.attachRemoteWorkbenchSessionForTarget(api, ref, true, expected)
	}
	return a.attachRemoteWorkbenchSessionForTarget(api, ref, true, expected)
}

func (a *App) attachRemoteWorkbenchCreatedSession(
	api runtimeapi.V1RuntimeAPI,
	created runtimeapi.CreatedSession,
	activate bool,
	expected TargetManagerSnapshot,
) (TabMeta, error) {
	return a.attachRemoteWorkbenchCreatedSessionMode(api, created, activate, expected, false, false)
}

var errRemoteSessionNotReusableBlank = errors.New("Remote Session is no longer a fresh blank Session")

func (a *App) attachRemoteWorkbenchBlankSession(
	api runtimeapi.V1RuntimeAPI,
	created runtimeapi.CreatedSession,
	expected TargetManagerSnapshot,
) (TabMeta, error) {
	return a.attachRemoteWorkbenchCreatedSessionMode(api, created, true, expected, true, false)
}

func (a *App) attachRemoteWorkbenchOpenBlankSession(
	api runtimeapi.V1RuntimeAPI,
	created runtimeapi.CreatedSession,
	expected TargetManagerSnapshot,
) (TabMeta, error) {
	return a.attachRemoteWorkbenchCreatedSessionMode(api, created, true, expected, true, true)
}

func (a *App) attachRemoteWorkbenchCreatedSessionMode(
	api runtimeapi.V1RuntimeAPI,
	created runtimeapi.CreatedSession,
	activate bool,
	expected TargetManagerSnapshot,
	requireFreshBlank bool,
	retainNonFresh bool,
) (TabMeta, error) {
	if api == nil || !created.Session.Valid() {
		return TabMeta{}, errors.New("a valid created Remote Session is required")
	}
	manager := a.remote.manager
	if manager == nil {
		return TabMeta{}, ErrRuntimeTargetUnavailable
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	workspace, err := remoteWorkspaceByID(ctx, api, created.Session.WorkspaceID)
	if err != nil {
		return TabMeta{}, err
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	attachInput := runtimeapi.AttachAndSubscribeInput{
		Session: created.Session, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
	}
	var snapshot runtimeapi.SessionSnapshot
	var rollback remoteWorkbenchSubscriptionRollback
	if requireFreshBlank {
		snapshot, rollback, err = attachRemoteWorkbenchFreshCandidateAtomic(ctx, api, attachInput)
	} else {
		snapshot, rollback, err = attachRemoteWorkbenchAtomic(ctx, api, attachInput)
	}
	if err != nil {
		return TabMeta{}, err
	}
	retainSubscription := false
	defer func() {
		if !retainSubscription {
			discardRemoteWorkbenchSubscription(rollback, created.Session)
		}
	}()
	if snapshot.Session != created.Session {
		return TabMeta{}, errors.New("Remote attach returned a different Session identity")
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	if requireFreshBlank && !remoteSnapshotIsReusableBlank(snapshot) {
		if !retainNonFresh {
			if open, _, current, ok := a.remoteSessionView(remoteSessionTabID(created.Session)); ok &&
				open.Created.Session == created.Session && remoteWorkbenchTargetMatches(current, expected) {
				retainNonFresh = true
			}
		}
		if retainNonFresh {
			created.TopicID = snapshot.TopicID
			created.TopicTitle = snapshot.Title
			created.ResolvedProfile = snapshot.Profile
			_, _, committed := a.commitRemoteWorkbenchSession(expected, workspace, created, snapshot, false)
			if !committed {
				return TabMeta{}, ErrTargetTransitionSuperseded
			}
			retainSubscription = true
			return TabMeta{}, errRemoteSessionNotReusableBlank
		}
		unsubscribeErr := runRemoteWorkbenchSubscriptionRollback(rollback)
		rollback = nil
		if unsubscribeErr != nil {
			return TabMeta{}, fmt.Errorf("Remote Session is not a reusable blank and could not be unsubscribed: %w", unsubscribeErr)
		}
		if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
			return TabMeta{}, ErrTargetTransitionSuperseded
		}
		return TabMeta{}, errRemoteSessionNotReusableBlank
	}
	created.TopicID = snapshot.TopicID
	created.TopicTitle = snapshot.Title
	created.ResolvedProfile = snapshot.Profile
	tabID, _, committed := a.commitRemoteWorkbenchSession(expected, workspace, created, snapshot, activate)
	if !committed {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	retainSubscription = true
	meta, ok := a.remoteTabMeta(tabID)
	if !ok {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	return meta, nil
}

func remoteWorkbenchTargetMatches(current, expected TargetManagerSnapshot) bool {
	return expected.State == TargetRemoteConnected && expected.Target.Kind == TargetRemote &&
		current.State == TargetRemoteConnected && current.Generation == expected.Generation &&
		sameTarget(current.Target, expected.Target)
}

// remoteSnapshotIsReusableBlank is the final authority for /new reuse. Catalog
// summaries are only a shortlist: the atomic attach snapshot must still be a
// completely fresh conversation with no foreground/background work or durable
// session-scoped state.
func remoteSnapshotIsReusableBlank(snapshot runtimeapi.SessionSnapshot) bool {
	history := snapshot.History
	runtime := snapshot.Runtime
	return history.TotalTurns == 0 && history.ActualTurns == 0 && history.StartTurn == 0 && history.EndTurn == 0 &&
		len(history.Messages) == 0 && !history.HasOlder && history.Next == "" &&
		!runtime.Running && runtime.CurrentTurn == nil && runtime.CurrentOperation == nil && !runtime.CancelRequested &&
		runtime.LastOutcome == "" && runtime.LastError == nil && runtime.Interruption == nil && len(runtime.LiveEvents) == 0 &&
		snapshot.PendingPrompt == nil && snapshot.Goal == nil && snapshot.GoalStatus == "" &&
		len(snapshot.Todos) == 0 && len(snapshot.Jobs) == 0 && len(snapshot.Checkpoints) == 0
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
	finishCreate, err := a.beginRemoteWorkbenchSessionCreate()
	if err != nil {
		return TabMeta{}, err
	}
	defer finishCreate()

	api, expected, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return TabMeta{}, err
	}
	manager := a.remote.manager
	if manager == nil {
		return TabMeta{}, ErrRuntimeTargetUnavailable
	}
	target := expected.Target
	workspace := runtimeapi.WorkspaceID(workspaceID)
	fingerprint := remoteBlankSessionCreateFingerprint(workspace)

	var reusableOpen *runtimeapi.CreatedSession
	openSessions := make(map[runtimeapi.SessionRef]bool)
	if !a.withRemoteWorkbenchTarget(expected, func(state *remoteWorkbenchState) {
		for _, tabID := range state.TabOrder {
			ref := state.SessionTabs[tabID]
			session := state.Sessions[ref]
			if session == nil || ref.WorkspaceID != workspace {
				continue
			}
			openSessions[ref] = true
			if reusableOpen == nil && remoteSnapshotIsReusableBlank(session.Snapshot) {
				created := session.Created
				reusableOpen = &created
				return
			}
		}
	}) {
		return TabMeta{}, ErrTargetTransitionSuperseded
	}
	pending := a.remoteWorkspacePending(target.ID, fingerprint)
	if pending != nil && pending.Workspace.ID != workspace {
		a.clearRemoteWorkspacePending(target.ID, fingerprint, pending.Created.Session)
		pending = nil
	}

	clearPending := func(ref runtimeapi.SessionRef) bool {
		return a.clearRemoteWorkspacePending(target.ID, fingerprint, ref)
	}
	createFresh := func() (TabMeta, error) {
		if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
			return TabMeta{}, ErrTargetTransitionSuperseded
		}
		ctx, cancel := a.remoteActionContext()
		created, createErr := api.CreateSession(ctx, runtimeapi.CreateSessionInput{
			WorkspaceID: workspace,
			Topic:       runtimeapi.TopicSelection{Kind: runtimeapi.TopicNew, Title: defaultTopicTitle},
		})
		cancel()
		if createErr != nil {
			return TabMeta{}, createErr
		}
		if !created.Session.Valid() || created.Session.WorkspaceID != workspace {
			return TabMeta{}, errors.New("Remote create returned a Session in a different workspace")
		}
		createdPending := &pendingRemoteSessionCreate{
			HostID: target.ID, Fingerprint: fingerprint, Workspace: runtimeapi.Workspace{ID: workspace}, Created: created,
		}
		// Persist the Host mutation before checking the active target. StateSink
		// may already have moved A -> B, but a later return to A must reuse this
		// exact opaque SessionRef even when the Host catalog is not yet visible.
		a.storeRemoteWorkspacePending(createdPending)
		if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
			return TabMeta{}, ErrTargetTransitionSuperseded
		}
		meta, attachErr := a.attachRemoteWorkbenchBlankSession(api, created, expected)
		if errors.Is(attachErr, errRemoteSessionNotReusableBlank) {
			if !clearPending(created.Session) {
				return TabMeta{}, ErrTargetTransitionSuperseded
			}
			return TabMeta{}, fmt.Errorf("newly created Remote Session was not fresh: %w", attachErr)
		}
		return meta, attachErr
	}

	if pending != nil {
		meta, attachErr := a.attachRemoteWorkbenchBlankSession(api, pending.Created, expected)
		if attachErr == nil {
			return meta, nil
		}
		if !errors.Is(attachErr, errRemoteSessionNotReusableBlank) {
			return TabMeta{}, attachErr
		}
		if !clearPending(pending.Created.Session) {
			return TabMeta{}, ErrTargetTransitionSuperseded
		}
		return createFresh()
	}
	if reusableOpen != nil {
		meta, attachErr := a.attachRemoteWorkbenchOpenBlankSession(api, *reusableOpen, expected)
		if attachErr == nil {
			return meta, nil
		}
		if !errors.Is(attachErr, errRemoteSessionNotReusableBlank) {
			return TabMeta{}, attachErr
		}
	}

	ctx, cancel := a.remoteActionContext()
	topics, listErr := listAllRemoteTopics(ctx, api, workspace)
	if listErr == nil {
		var sessions []runtimeapi.SessionSummary
		sessions, listErr = listAllRemoteSessions(ctx, api, workspace)
		if listErr == nil {
			if reusable, ok := newestReusableRemoteBlankSessionExcluding(sessions, topics, openSessions); ok {
				cancel()
				created := runtimeapi.CreatedSession{
					Session: reusable.Session, TopicID: reusable.TopicID, TopicTitle: reusable.Title,
				}
				meta, attachErr := a.attachRemoteWorkbenchBlankSession(api, created, expected)
				if attachErr == nil {
					return meta, nil
				}
				if !errors.Is(attachErr, errRemoteSessionNotReusableBlank) {
					return TabMeta{}, attachErr
				}
				return createFresh()
			}
		}
	}
	cancel()
	if listErr != nil {
		return TabMeta{}, listErr
	}
	return createFresh()
}

func newestReusableRemoteBlankSession(
	sessions []runtimeapi.SessionSummary,
	topics []runtimeapi.TopicSummary,
) (runtimeapi.SessionSummary, bool) {
	return newestReusableRemoteBlankSessionExcluding(sessions, topics, nil)
}

func newestReusableRemoteBlankSessionExcluding(
	sessions []runtimeapi.SessionSummary,
	topics []runtimeapi.TopicSummary,
	excluded map[runtimeapi.SessionRef]bool,
) (runtimeapi.SessionSummary, bool) {
	topicSessionCounts := make(map[runtimeapi.TopicID]int, len(topics))
	for _, topic := range topics {
		topicSessionCounts[topic.TopicID] = topic.SessionCount
	}
	var selected runtimeapi.SessionSummary
	found := false
	for _, session := range sessions {
		if excluded[session.Session] || !session.Session.Valid() || session.Turns != 0 || strings.TrimSpace(session.Preview) != "" ||
			session.BranchSource != nil || session.RecoveryInterrupted || topicSessionCounts[session.TopicID] != 1 {
			continue
		}
		if runtime := session.Runtime; runtime != nil && (runtime.Running || runtime.PendingPrompt || runtime.ActiveJobs != 0) {
			continue
		}
		if !found || session.LastActivityMillis > selected.LastActivityMillis ||
			(session.LastActivityMillis == selected.LastActivityMillis && session.CreatedAtMillis > selected.CreatedAtMillis) {
			selected = session
			found = true
		}
	}
	return selected, found
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
	api, expected, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return TopicMeta{}, err
	}
	ctx, cancel := a.remoteActionContext()
	created, err := api.CreateTopic(ctx, runtimeapi.CreateTopicInput{WorkspaceID: runtimeapi.WorkspaceID(workspaceID), Title: strings.TrimSpace(title)})
	cancel()
	if err != nil {
		return TopicMeta{}, err
	}
	if a.remote.manager == nil || !remoteWorkbenchTargetMatches(a.remote.manager.Snapshot(), expected) {
		return TopicMeta{}, ErrTargetTransitionSuperseded
	}
	a.emitProjectTreeChanged()
	return TopicMeta{ID: string(created.TopicID), Title: created.Title, CreatedAt: created.CreatedAtMillis}, nil
}

func (a *App) remoteRenameTopic(topicID, title string) error {
	api, expected, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return err
	}
	lookupCtx, lookupCancel := a.remoteActionContext()
	workspaceID, err := a.remoteWorkspaceForTopic(lookupCtx, api, expected, runtimeapi.TopicID(strings.TrimSpace(topicID)))
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
	if !a.withRemoteWorkbenchTarget(expected, func(state *remoteWorkbenchState) {
		for ref, session := range state.Sessions {
			if session != nil && ref.WorkspaceID == workspaceID && session.Created.TopicID == runtimeapi.TopicID(topicID) {
				session.Created.TopicTitle = result.Title
				session.Snapshot.Title = result.Title
			}
		}
	}) {
		return ErrTargetTransitionSuperseded
	}
	a.publishRemoteWorkbenchChanged()
	return nil
}

func (a *App) remoteDeleteTopic(topicID string) error {
	api, expected, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return err
	}
	id := runtimeapi.TopicID(strings.TrimSpace(topicID))
	lookupCtx, lookupCancel := a.remoteActionContext()
	workspaceID, err := a.remoteWorkspaceForTopic(lookupCtx, api, expected, id)
	lookupCancel()
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.DeleteTopic(ctx, runtimeapi.DeleteTopicInput{WorkspaceID: workspaceID, TopicID: id})
	cancel()
	if err == nil && (a.remote.manager == nil || !remoteWorkbenchTargetMatches(a.remote.manager.Snapshot(), expected)) {
		return ErrTargetTransitionSuperseded
	}
	if err == nil {
		a.emitProjectTreeChanged()
	}
	return err
}

func (a *App) remoteTrashTopic(topicID string) error {
	a.remote.workbenchOp.Lock()
	defer a.remote.workbenchOp.Unlock()

	api, target, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return err
	}
	id := runtimeapi.TopicID(strings.TrimSpace(topicID))
	lookupCtx, lookupCancel := a.remoteActionContext()
	workspaceID, err := a.remoteWorkspaceForTopic(lookupCtx, api, target, id)
	lookupCancel()
	if err != nil {
		return err
	}
	matching := make([]runtimeapi.SessionRef, 0)
	var activeErr error
	if !a.withRemoteWorkbenchTarget(target, func(state *remoteWorkbenchState) {
		for _, tabID := range state.TabOrder {
			ref, exists := state.SessionTabs[tabID]
			if !exists {
				continue
			}
			session := state.Sessions[ref]
			if session == nil || ref.WorkspaceID != workspaceID || session.Created.TopicID != id {
				continue
			}
			if session.Snapshot.Runtime.Running || session.Snapshot.Runtime.CurrentTurn != nil ||
				session.Snapshot.Runtime.CurrentOperation != nil || session.Snapshot.PendingPrompt != nil || len(session.Snapshot.Jobs) != 0 {
				activeErr = errors.New("Remote topic has active work; cancel its Turn, prompt, Operation, and jobs before moving it to trash")
				return
			}
			matching = append(matching, ref)
		}
	}) {
		return ErrTargetTransitionSuperseded
	}
	if activeErr != nil {
		return activeErr
	}

	unsubscribed := make([]runtimeapi.SessionRef, 0, len(matching))
	for _, ref := range matching {
		if !remoteWorkbenchTargetMatches(a.remote.manager.Snapshot(), target) {
			return ErrTargetTransitionSuperseded
		}
		ctx, cancel := a.remoteActionContext()
		unsubscribeErr := api.UnsubscribeSession(ctx, runtimeapi.UnsubscribeSessionInput{Session: ref})
		cancel()
		if unsubscribeErr != nil {
			rollbackErr := a.restoreRemoteTopicSubscriptions(api, unsubscribed, target)
			if rollbackErr != nil {
				return fmt.Errorf("unsubscribe Remote topic before trash: %w (subscription rollback failed: %v)", unsubscribeErr, rollbackErr)
			}
			return fmt.Errorf("unsubscribe Remote topic before trash: %w", unsubscribeErr)
		}
		unsubscribed = append(unsubscribed, ref)
	}
	if !remoteWorkbenchTargetMatches(a.remote.manager.Snapshot(), target) {
		return ErrTargetTransitionSuperseded
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.TrashTopic(ctx, runtimeapi.TrashTopicInput{WorkspaceID: workspaceID, TopicID: id})
	cancel()
	if err != nil {
		rollbackErr := a.restoreRemoteTopicSubscriptions(api, unsubscribed, target)
		if rollbackErr != nil {
			return fmt.Errorf("trash Remote topic: %w (subscription rollback failed: %v)", err, rollbackErr)
		}
		return err
	}
	if _, committed := a.removeRemoteWorkbenchTopic(target, workspaceID, id); !committed {
		return ErrTargetTransitionSuperseded
	}
	return nil
}

func (a *App) restoreRemoteTopicSubscriptions(api runtimeapi.V1RuntimeAPI, refs []runtimeapi.SessionRef, expected TargetManagerSnapshot) error {
	var restoreErrors []error
	for _, ref := range refs {
		ctx, cancel := a.remoteActionContext()
		snapshot, rollback, err := attachRemoteWorkbenchAtomic(ctx, api, runtimeapi.AttachAndSubscribeInput{
			Session: ref, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
		})
		cancel()
		if err == nil && snapshot.Session != ref {
			err = errors.New("Remote rollback attach returned a different Session identity")
		}
		currentTarget := a.remote.manager.Snapshot()
		if err == nil && !remoteWorkbenchTargetMatches(currentTarget, expected) {
			err = ErrTargetTransitionSuperseded
		}
		committed := false
		a.remote.workbenchMu.Lock()
		if a.remote.workbench.HostID == expected.Target.ID {
			if session := a.remote.workbench.Sessions[ref]; err == nil && session != nil && session.AttachedGeneration == expected.Generation {
				session.Snapshot = cloneRemoteSessionSnapshot(snapshot)
				session.HistoryCursors = remoteHistoryCursorsFromSnapshot(snapshot)
				session.Created.TopicID = snapshot.TopicID
				session.Created.TopicTitle = snapshot.Title
				session.Created.ResolvedProfile = snapshot.Profile
				session.LastAttachError = ""
				committed = true
			}
		}
		a.remote.workbenchMu.Unlock()
		if err == nil && !committed {
			err = ErrTargetTransitionSuperseded
		}
		if err != nil {
			if rollback != nil {
				discardRemoteWorkbenchSubscription(rollback, ref)
			}
			restoreErrors = append(restoreErrors, fmt.Errorf("%s: %w", ref.SessionID, err))
		}
	}
	return errors.Join(restoreErrors...)
}

func (a *App) remoteWorkspaceForTopic(
	ctx context.Context,
	api runtimeapi.V1RuntimeAPI,
	expected TargetManagerSnapshot,
	topicID runtimeapi.TopicID,
) (runtimeapi.WorkspaceID, error) {
	if topicID == "" {
		return "", errors.New("Remote topic ID is required")
	}
	var match runtimeapi.WorkspaceID
	if !a.withRemoteWorkbenchTarget(expected, func(state *remoteWorkbenchState) {
		for ref, session := range state.Sessions {
			if session != nil && session.Created.TopicID == topicID {
				match = ref.WorkspaceID
				return
			}
		}
	}) {
		return "", ErrTargetTransitionSuperseded
	}
	if match != "" {
		return match, nil
	}
	workspaces, err := listAllRemoteWorkspaces(ctx, api)
	if err != nil {
		return "", err
	}
	for _, workspace := range workspaces {
		if a.remote.manager == nil || !remoteWorkbenchTargetMatches(a.remote.manager.Snapshot(), expected) {
			return "", ErrTargetTransitionSuperseded
		}
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
	if a.remote.manager == nil || !remoteWorkbenchTargetMatches(a.remote.manager.Snapshot(), expected) {
		return "", ErrTargetTransitionSuperseded
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
			for _, summary := range sessions {
				if summary.TopicID != topic.TopicID {
					continue
				}
				isOpen, isRunning, status := a.remoteSessionCatalogState(summary)
				if summary.Turns > turns {
					turns = summary.Turns
				}
				sessionNodes = append(sessionNodes, ProjectNode{
					Key: "remote_session_" + remoteSessionTabID(summary.Session), Kind: "session",
					Label: summary.Title, Root: string(workspace.ID), TopicID: string(summary.TopicID),
					SessionPath: remoteSessionToken(summary.Session), Turns: summary.Turns,
					CreatedAt: summary.CreatedAtMillis, LastActivityAt: summary.LastActivityMillis,
					Open: isOpen, Running: isRunning, Status: status, Recovered: summary.RecoveryInterrupted,
				})
			}
			// Match Local projection semantics. With one runtime Session the Topic
			// row is that Session's navigation/status surface. Once a topic has
			// multiple Sessions, expose concrete children and stop presenting an
			// aggregate runtime state on the parent row.
			var topicChildren []ProjectNode
			var open, running bool
			var status string
			if len(sessionNodes) == 1 {
				open, running, status = sessionNodes[0].Open, sessionNodes[0].Running, sessionNodes[0].Status
			} else if len(sessionNodes) > 1 {
				topicChildren = sessionNodes
			}
			children = append(children, ProjectNode{
				Key: "remote_topic_" + string(topic.TopicID), Kind: "topic", Label: topic.Title,
				Root: string(workspace.ID), TopicID: string(topic.TopicID), Turns: turns,
				CreatedAt: topic.CreatedAtMillis, LastActivityAt: topic.LastActivityMillis,
				Open: open, Running: running, Status: status, Children: topicChildren,
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
