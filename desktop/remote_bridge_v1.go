package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/runtimeapi"
)

// remote_bridge_v1.go is the legacy Wails-to-RuntimeAPI projection.  The
// Wails signatures intentionally remain source-compatible with the existing
// workbench, while every Host operation below is expressed exclusively in the
// target-neutral V1 contract.  In particular, strings historically named
// "path" are opaque Remote Session/Workspace/Document identifiers here and
// are never passed to Desktop filesystem helpers.

const remoteLegacyListMaxPages = 64

func advanceRemoteLegacyCursor(method string, current, next runtimeapi.Cursor, hasMore bool, seen map[runtimeapi.Cursor]struct{}, pages int) (runtimeapi.Cursor, bool, error) {
	if !hasMore {
		if next != "" {
			return "", false, fmt.Errorf("%s returned a cursor without hasMore", method)
		}
		return "", false, nil
	}
	if next == "" || next == current {
		return "", false, fmt.Errorf("%s returned a missing or non-advancing cursor", method)
	}
	if _, duplicate := seen[next]; duplicate {
		return "", false, fmt.Errorf("%s returned a repeated cursor", method)
	}
	if pages >= remoteLegacyListMaxPages {
		return "", false, fmt.Errorf("%s exceeded the Desktop legacy list budget of %d pages", method, remoteLegacyListMaxPages)
	}
	seen[next] = struct{}{}
	return next, true, nil
}

func (a *App) remoteComposerSubmitV1(tabID string, input runtimeapi.ComposerSubmitInput) error {
	api, session, _, target, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	input.Session = session.Created.Session
	ctx, cancel := a.remoteActionContext()
	result, err := api.ComposerSubmit(ctx, input)
	cancel()
	if err != nil {
		return err
	}
	if result.Session.Valid() && result.Session != session.Created.Session && result.Effect != string(runtimeapi.EffectSessionReplaced) {
		return errors.New("Remote composer returned an unexpected Session identity")
	}
	if result.SnapshotRequired && result.Effect == string(runtimeapi.EffectStateChanged) {
		_, err = a.refreshRemoteWorkbenchSession(api, session.Created.Session, target.Generation)
		return err
	}
	if !a.recordRemoteSubmitResult(session.Created.Session, target.Generation, result) {
		return ErrTargetTransitionSuperseded
	}
	a.emitProjectTreeChanged()
	return nil
}

func remoteInvocationInputs(inputs []InvocationRequest) ([]runtimeapi.Invocation, error) {
	out := make([]runtimeapi.Invocation, len(inputs))
	for index, input := range inputs {
		kind := runtimeapi.InvocationKind(strings.TrimSpace(input.Kind))
		if kind != runtimeapi.InvocationSkill && kind != runtimeapi.InvocationSubagent {
			return nil, fmt.Errorf("unsupported Remote invocation kind %q", input.Kind)
		}
		name := strings.TrimSpace(input.Name)
		if name == "" {
			return nil, errors.New("Remote invocation name is required")
		}
		out[index] = runtimeapi.Invocation{Name: name, Kind: kind}
	}
	return out, nil
}

func (a *App) remoteRunShellV1(tabID, command string) error {
	api, session, _, target, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.RunShell(ctx, runtimeapi.RunShellInput{Session: session.Created.Session, Command: command})
	cancel()
	if err == nil && !a.recordRemoteOperationStarted(session.Created.Session, target.Generation, runtimeapi.OperationShell, result) {
		return ErrTargetTransitionSuperseded
	}
	return err
}

func (a *App) remoteCompactV1(tabID string) error {
	api, session, _, target, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.CompactSession(ctx, runtimeapi.CompactSessionInput{Session: session.Created.Session})
	cancel()
	if err == nil && !a.recordRemoteOperationStarted(session.Created.Session, target.Generation, runtimeapi.OperationCompact, result) {
		return ErrTargetTransitionSuperseded
	}
	return err
}

func (a *App) remoteNewSessionV1(tabID string) error {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.NewSession(ctx, runtimeapi.SessionActionInput{Session: session.Created.Session})
	cancel()
	if err != nil {
		return err
	}
	if result.Source != session.Created.Session || !result.Session.Valid() || !result.SnapshotRequired {
		return errors.New("Remote new Session returned an invalid replacement result")
	}
	return nil // ordered SnapshotUpdate atomically migrates the tab binding
}

func (a *App) remoteClearSessionV1(tabID string) error {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.ClearSession(ctx, runtimeapi.SessionActionInput{Session: session.Created.Session})
	cancel()
	if err != nil {
		return err
	}
	if result.Previous != session.Created.Session || !result.Session.Valid() || !result.SnapshotRequired {
		return errors.New("Remote clear Session returned an invalid replacement result")
	}
	return nil // ordered SnapshotUpdate atomically migrates the tab binding
}

func remoteCheckpointID(snapshot runtimeapi.SessionSnapshot, displayTurn int) (runtimeapi.CheckpointID, error) {
	for _, checkpoint := range snapshot.Checkpoints {
		if checkpoint.DisplayTurn == displayTurn {
			if checkpoint.ID == "" {
				break
			}
			return checkpoint.ID, nil
		}
	}
	return "", fmt.Errorf("Remote checkpoint for display turn %d is unavailable", displayTurn)
}

func (a *App) remoteRewindV1(tabID string, turn int, scope string) error {
	api, session, _, target, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	checkpointID, err := remoteCheckpointID(session.Snapshot, turn)
	if err != nil {
		return err
	}
	rewindScope := runtimeapi.RewindBoth
	switch strings.TrimSpace(scope) {
	case "code":
		rewindScope = runtimeapi.RewindCode
	case "conversation":
		rewindScope = runtimeapi.RewindConversation
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.RewindSession(ctx, runtimeapi.RewindSessionInput{
		Session: session.Created.Session, CheckpointID: checkpointID, Scope: rewindScope,
	})
	cancel()
	if err != nil {
		return err
	}
	if result.SnapshotRequired {
		_, err = a.refreshRemoteWorkbenchSession(api, session.Created.Session, target.Generation)
	}
	return err
}

func (a *App) remoteForkV1(tabID string, turn int) (TabMeta, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return TabMeta{}, err
	}
	checkpointID, err := remoteCheckpointID(session.Snapshot, turn)
	if err != nil {
		return TabMeta{}, err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.ForkSession(ctx, runtimeapi.ForkSessionInput{Session: session.Created.Session, CheckpointID: checkpointID})
	cancel()
	if err != nil {
		return TabMeta{}, err
	}
	if result.Source != session.Created.Session || !result.Child.Valid() {
		return TabMeta{}, errors.New("Remote fork returned an invalid child Session")
	}
	return a.attachRemoteWorkbenchSession(result.Child, true)
}

func (a *App) remoteSummarizeV1(tabID string, turn int, direction runtimeapi.SummaryDirection) error {
	api, session, _, target, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	checkpointID, err := remoteCheckpointID(session.Snapshot, turn)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.SummarizeSession(ctx, runtimeapi.SummarizeSessionInput{
		Session: session.Created.Session, CheckpointID: checkpointID, Direction: direction,
	})
	cancel()
	if err == nil && !a.recordRemoteOperationStarted(session.Created.Session, target.Generation, runtimeapi.OperationSummarize, result) {
		return ErrTargetTransitionSuperseded
	}
	return err
}

func (a *App) remoteSetProfileV1(tabID string, patch runtimeapi.ProfilePatch) (runtimeapi.SetProfileResult, error) {
	api, session, _, target, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return runtimeapi.SetProfileResult{}, err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.SetProfile(ctx, runtimeapi.SetProfileInput{Session: session.Created.Session, Patch: patch})
	cancel()
	if err != nil {
		return runtimeapi.SetProfileResult{}, err
	}
	if !result.SnapshotRequired && !a.recordRemoteProfileResult(session.Created.Session, target.Generation, result) {
		err = ErrTargetTransitionSuperseded
	}
	// A rebuilt profile changes runtimeEpoch. The adapter's ordered resync path
	// owns its SnapshotUpdate; refreshing the old epoch here would race that
	// migration and can never be authoritative.
	return result, err
}

func (a *App) remoteSetGoalV1(tabID, goal string) error {
	api, session, _, target, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	if strings.TrimSpace(goal) == "" {
		_, err = api.ClearGoal(ctx, runtimeapi.ClearGoalInput{Session: session.Created.Session})
	} else {
		_, err = api.SetGoal(ctx, runtimeapi.SetGoalInput{Session: session.Created.Session, Goal: strings.TrimSpace(goal)})
	}
	cancel()
	if err == nil {
		_, err = a.refreshRemoteWorkbenchSession(api, session.Created.Session, target.Generation)
	}
	return err
}

func (a *App) remoteResumeGoalV1(tabID string) (bool, error) {
	api, session, _, target, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return false, err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.ResumeGoal(ctx, runtimeapi.ResumeGoalInput{Session: session.Created.Session})
	cancel()
	if err == nil {
		_, err = a.refreshRemoteWorkbenchSession(api, session.Created.Session, target.Generation)
	}
	return result.Resumed, err
}

func remoteSessionToken(ref runtimeapi.SessionRef) string { return string(ref.SessionID) }

func (a *App) remoteSessionRefForToken(tabID, token string) (runtimeapi.SessionRef, error) {
	_, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return runtimeapi.SessionRef{}, err
	}
	id := runtimeapi.SessionID(strings.TrimSpace(token))
	if id == "" {
		return runtimeapi.SessionRef{}, errors.New("Remote Session identity is required")
	}
	return runtimeapi.SessionRef{WorkspaceID: session.Created.Session.WorkspaceID, SessionID: id}, nil
}

func remoteSessionMeta(item runtimeapi.SessionSummary, current runtimeapi.SessionRef) SessionMeta {
	return SessionMeta{
		Path: remoteSessionToken(item.Session), Preview: item.Preview, Title: item.Title, Turns: item.Turns,
		CreatedAt: item.CreatedAtMillis, LastActivityAt: item.LastActivityMillis, ModTime: item.LastActivityMillis,
		Current: item.Session == current, Open: item.Runtime != nil, Scope: "project",
		TopicID: string(item.TopicID), TopicTitle: item.Title, Recovered: item.RecoveryInterrupted,
	}
}

func (a *App) remoteListSessionsV1(tabID string) ([]SessionMeta, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	out := []SessionMeta{}
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	for pages := 1; ; pages++ {
		page, callErr := api.ListSessions(ctx, runtimeapi.ListSessionsInput{
			WorkspaceID: session.Created.Session.WorkspaceID, Cursor: cursor, Limit: runtimeapi.PageMaxItems,
		})
		if callErr != nil {
			return nil, callErr
		}
		for _, item := range page.Items {
			out = append(out, remoteSessionMeta(item, session.Created.Session))
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("session/list", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return nil, cursorErr
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
}

func (a *App) remoteListTrashV1(tabID string) ([]SessionMeta, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	out := []SessionMeta{}
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	for pages := 1; ; pages++ {
		page, callErr := api.ListTrashedSessions(ctx, runtimeapi.ListTrashedSessionsInput{
			WorkspaceID: session.Created.Session.WorkspaceID, Cursor: cursor, Limit: runtimeapi.PageMaxItems,
		})
		if callErr != nil {
			return nil, callErr
		}
		for _, item := range page.Items {
			out = append(out, SessionMeta{
				Path: remoteSessionToken(item.Session), Preview: item.Preview, Title: item.Title,
				DeletedAt: item.TrashedAtMillis, Scope: "project", TopicID: string(item.TopicID),
				TopicTitle: item.Title, RecoveryCopy: item.RecoveryCopy,
			})
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("session/trashList", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return nil, cursorErr
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
}

func (a *App) remoteTrashSessionV1(tabID, token string, redundantOnly bool) error {
	api, _, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ref, err := a.remoteSessionRefForToken(tabID, token)
	if err != nil {
		return err
	}
	guard := runtimeapi.TrashNormal
	if redundantOnly {
		guard = runtimeapi.TrashRedundantRecoveryOnly
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.TrashSession(ctx, runtimeapi.TrashSessionInput{Session: ref, Guard: guard})
	cancel()
	if err == nil {
		a.removeRemoteWorkbenchSession(ref)
	}
	return err
}

func (a *App) remoteRestoreSessionV1(tabID, token string) error {
	api, _, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ref, err := a.remoteSessionRefForToken(tabID, token)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.RestoreSession(ctx, runtimeapi.RestoreSessionInput{Session: ref})
	cancel()
	return err
}

func (a *App) remotePurgeSessionV1(tabID, token string, redundantOnly bool) error {
	api, _, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ref, err := a.remoteSessionRefForToken(tabID, token)
	if err != nil {
		return err
	}
	guard := runtimeapi.TrashNormal
	if redundantOnly {
		guard = runtimeapi.TrashRedundantRecoveryOnly
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.PurgeSession(ctx, runtimeapi.PurgeSessionInput{Session: ref, Guard: guard})
	cancel()
	return err
}

func (a *App) remoteRenameSessionV1(tabID, token, title string) error {
	api, _, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ref, err := a.remoteSessionRefForToken(tabID, token)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.RenameSession(ctx, runtimeapi.RenameSessionInput{Session: ref, Title: title})
	cancel()
	return err
}

func (a *App) remoteResumeSessionV1(tabID, token string) (HistoryPage, error) {
	ref, err := a.remoteSessionRefForToken(tabID, token)
	if err != nil {
		return HistoryPage{}, err
	}
	if _, err = a.attachRemoteWorkbenchSession(ref, true); err != nil {
		return HistoryPage{}, err
	}
	page, ok := a.remoteHistoryPage(remoteSessionTabID(ref))
	if !ok {
		return HistoryPage{}, errors.New("Remote Session attached without a workbench snapshot")
	}
	return page, nil
}

func (a *App) remotePreviewSessionV1(tabID, token string) ([]HistoryMessage, error) {
	api, _, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ref, err := a.remoteSessionRefForToken(tabID, token)
	if err != nil {
		return nil, err
	}
	// Previewing an already-open Session must be a pure projection. A second
	// AttachAndSubscribe would replace its atomic live subscription and the
	// paired preview cleanup would then silently detach the workbench tab.
	if open, _, _, ok := a.remoteSessionView(remoteSessionTabID(ref)); ok && open.Created.Session == ref {
		return mapRemoteHistoryMessages(open.Snapshot.History.Messages, open.Snapshot.Checkpoints), nil
	}
	ctx, cancel := a.remoteActionContext()
	snapshot, err := api.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: ref, HistoryTurns: a.desktopHistoryPageTurns(),
	})
	if err == nil {
		err = api.UnsubscribeSession(ctx, runtimeapi.UnsubscribeSessionInput{Session: ref})
	}
	cancel()
	if err != nil {
		return nil, err
	}
	return mapRemoteHistoryMessages(snapshot.History.Messages, snapshot.Checkpoints), nil
}

func (a *App) remotePromptHistoryV1(rawRequest string) (PromptHistoryResult, error) {
	api, session, _, _, err := a.remoteV1ForTab("")
	if err != nil {
		return PromptHistoryResult{}, err
	}
	req := parsePromptHistoryRequest(rawRequest)
	ctx, cancel := a.remoteActionContext()
	page, err := api.ComposerHistory(ctx, runtimeapi.PromptHistoryInput{
		WorkspaceID: session.Created.Session.WorkspaceID, Cursor: runtimeapi.Cursor(req.Cursor), Limit: promptHistoryLimit(req.Limit),
	})
	cancel()
	if err != nil {
		return PromptHistoryResult{}, err
	}
	entries := make([]PromptHistoryEntry, len(page.Entries))
	for index, entry := range page.Entries {
		entries[index] = PromptHistoryEntry{
			Text: entry.Text, At: entry.AtMillis, SessionPath: remoteSessionToken(entry.Session), Turn: entry.Turn,
		}
	}
	return PromptHistoryResult{
		Entries: entries, Nonce: string(session.Created.Session.WorkspaceID),
		OlderCursor: string(page.Next), HasOlder: page.HasMore,
	}, nil
}

func (a *App) remoteHistoryPageV1(tabID string, beforeTurn, limit int) (HistoryPage, error) {
	session, _, _, ok := a.remoteSessionView(tabID)
	if !ok {
		return HistoryPage{Messages: []HistoryMessage{}}, ErrRuntimeTargetUnavailable
	}
	if beforeTurn <= 0 || beforeTurn >= session.Snapshot.History.TotalTurns {
		page, ok := a.remoteHistoryPage(tabID)
		if !ok {
			return HistoryPage{Messages: []HistoryMessage{}}, ErrRuntimeTargetUnavailable
		}
		return page, nil
	}
	cursor, ref, ok := a.remoteHistoryCursor(tabID, beforeTurn)
	if !ok || ref != session.Created.Session {
		return HistoryPage{Messages: []HistoryMessage{}}, fmt.Errorf("Remote history cursor for turn %d is stale or unavailable", beforeTurn)
	}
	api, _, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return HistoryPage{Messages: []HistoryMessage{}}, err
	}
	limit = normalizeHistoryPageLimit(limit)
	ctx, cancel := a.remoteActionContext()
	page, err := api.SessionHistory(ctx, runtimeapi.HistoryInput{Session: ref, Cursor: cursor, PageTurns: limit})
	cancel()
	if err != nil {
		return HistoryPage{Messages: []HistoryMessage{}}, err
	}
	a.recordRemoteHistoryPage(ref, page)
	return HistoryPage{
		Messages:  mapRemoteHistoryMessages(page.Messages, session.Snapshot.Checkpoints),
		StartTurn: page.StartTurn, EndTurn: page.EndTurn, TotalTurns: page.TotalTurns, HasOlder: page.HasOlder,
	}, nil
}

func (a *App) remoteSlashArgsV1(input string) (SlashArgsResult, error) {
	api, session, _, _, err := a.remoteV1ForTab("")
	if err != nil {
		return SlashArgsResult{Items: []SlashArgItem{}}, err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.ComposerSlashArgs(ctx, runtimeapi.SlashArgsInput{Session: session.Created.Session, Input: input})
	cancel()
	if err != nil {
		return SlashArgsResult{Items: []SlashArgItem{}}, err
	}
	out := SlashArgsResult{Items: make([]SlashArgItem, len(result.Items)), From: result.From}
	for index, item := range result.Items {
		out.Items[index] = SlashArgItem{Label: item.Label, Insert: item.Insert, Hint: item.Hint, Descend: item.Descend}
	}
	return out, nil
}

func remoteContextInfo(view runtimeapi.ContextView) ContextInfo {
	sources := make(map[string]usageSourceStats, len(view.Sources))
	for _, source := range view.Sources {
		sources[source.Source] = usageSourceStats{
			PromptTokens: source.PromptTokens, CompletionTokens: source.CompletionTokens,
			TotalTokens: source.TotalTokens, ReasoningTokens: source.ReasoningTokens,
			CacheHitTokens: source.CacheHitTokens, CacheMissTokens: source.CacheMissTokens,
			RequestCount: source.RequestCount, SessionCost: source.SessionCost, SessionCurrency: source.SessionCurrency,
		}
	}
	return ContextInfo{
		Used: view.UsedTokens, Window: view.WindowTokens, SessionTokens: view.TotalTokens,
		SessionCost: view.SessionCost, SessionCurrency: view.SessionCurrency,
		CacheHitTokens: view.SessionCacheHitTokens, CacheMissTokens: view.SessionCacheMissTokens, Sources: sources,
	}
}

func (a *App) remoteContextV1(tabID string) (ContextInfo, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return ContextInfo{}, err
	}
	ctx, cancel := a.remoteActionContext()
	view, err := api.SessionContext(ctx, runtimeapi.SessionContextInput{Session: session.Created.Session})
	cancel()
	return remoteContextInfo(view), err
}

func (a *App) remoteBalanceV1(tabID string) (BalanceInfo, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return BalanceInfo{}, err
	}
	ctx, cancel := a.remoteActionContext()
	view, err := api.SessionBalance(ctx, runtimeapi.SessionBalanceInput{Session: session.Created.Session})
	cancel()
	if err != nil {
		return BalanceInfo{}, err
	}
	return BalanceInfo{Available: view.Available, Display: view.Display}, nil
}

func (a *App) remoteJobsV1(tabID string) ([]JobView, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	out := []JobView{}
	for pages := 1; ; pages++ {
		page, err := api.ListJobs(ctx, runtimeapi.ListJobsInput{
			Session: session.Created.Session, Cursor: cursor, Limit: runtimeapi.PageMaxItems,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range page.Jobs {
			out = append(out, JobView{
				ID: string(item.ID), Kind: string(item.Kind), Label: item.Label,
				Status: string(item.Status), StartedAt: item.StartedAtMillis,
			})
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("job/list", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return nil, cursorErr
		}
		if !more {
			break
		}
		cursor = next
	}
	return out, nil
}

func (a *App) remoteToolResultV1(tabID, toolID string) *control.ToolResultData {
	session, _, _, ok := a.remoteSessionView(tabID)
	if !ok {
		return nil
	}
	if result := remoteToolResultFromSnapshot(session.Snapshot, toolID); result != nil {
		return result
	}
	// Turn completion clears the bounded live-event tape. Refresh only an idle
	// same-session binding on a miss so archived output is read from the Host's
	// authoritative hydrated snapshot without disturbing an active Turn.
	if !session.Snapshot.Runtime.Running {
		if api, _, _, target, err := a.remoteV1ForTab(tabID); err == nil {
			if fresh, refreshErr := a.refreshRemoteWorkbenchSession(api, session.Created.Session, target.Generation); refreshErr == nil {
				return remoteToolResultFromSnapshot(fresh, toolID)
			}
		}
	}
	return nil
}

func remoteToolResultFromSnapshot(snapshot runtimeapi.SessionSnapshot, toolID string) *control.ToolResultData {
	var live *control.ToolResultData
	for index := len(snapshot.Runtime.LiveEvents) - 1; index >= 0; index-- {
		event := snapshot.Runtime.LiveEvents[index]
		if event.Tool == nil || event.Tool.ID != toolID {
			continue
		}
		if live == nil {
			live = &control.ToolResultData{}
		}
		if live.Args == "" && event.Tool.Args != "" {
			live.Args = event.Tool.Args
		}
		if live.Output == "" {
			live.Output = event.Tool.Output
			if live.Output == "" {
				live.Output = event.Tool.Err
			}
		}
	}
	if live != nil && (live.Args != "" || live.Output != "") {
		return live
	}
	messages := snapshot.History.Messages
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != "tool" || message.ToolCallID != toolID {
			continue
		}
		out := &control.ToolResultData{Output: dereferenceString(message.Content)}
		for prior := index - 1; prior >= 0; prior-- {
			for _, call := range messages[prior].ToolCalls {
				if call.ID == toolID {
					out.Args = dereferenceString(call.Arguments)
					return out
				}
			}
		}
		return out
	}
	return nil
}

func mapRemoteMemoryView(view runtimeapi.MemoryView) MemoryView {
	out := MemoryView{
		Docs: []MemoryDoc{}, Facts: []MemoryFact{}, Archives: []MemoryArchive{}, Scopes: []MemoryScope{}, Available: view.Available,
	}
	for _, document := range view.Documents {
		out.Docs = append(out.Docs, MemoryDoc{Path: string(document.DocumentID), Scope: document.Scope, Body: dereferenceString(document.Body)})
	}
	for _, fact := range view.Facts {
		title := fact.Title
		if title == "" {
			title = fact.Name
		}
		out.Facts = append(out.Facts, MemoryFact{
			Name: string(fact.MemoryID), Title: title, Description: fact.Description, Type: fact.Type, Body: dereferenceString(fact.Body),
		})
	}
	for _, archive := range view.Archives {
		title := archive.Title
		if title == "" {
			title = archive.Name
		}
		out.Archives = append(out.Archives, MemoryArchive{
			Name: string(archive.MemoryID), Title: title, Description: archive.Description, Type: archive.Type,
			Body: dereferenceString(archive.Body), Path: string(archive.MemoryID), ArchivedAt: archive.ArchivedAt,
		})
	}
	for _, scope := range view.Scopes {
		out.Scopes = append(out.Scopes, MemoryScope{Scope: scope.Scope, Path: scope.DisplayPath})
	}
	return out
}

func (a *App) remoteMemoryV1(tabID string) (MemoryView, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return mapRemoteMemoryView(runtimeapi.MemoryView{}), err
	}
	ctx, cancel := a.remoteActionContext()
	view, err := api.Memory(ctx, runtimeapi.MemoryInput{Session: session.Created.Session})
	cancel()
	return mapRemoteMemoryView(view), err
}

func (a *App) remoteRememberV1(tabID, scope, note string) (string, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return "", err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.RememberMemory(ctx, runtimeapi.RememberMemoryInput{Session: session.Created.Session, Scope: scope, Note: note})
	cancel()
	return result.DisplayPath, err
}

func (a *App) remoteForgetV1(tabID, memoryID string) error {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.ForgetMemory(ctx, runtimeapi.ForgetMemoryInput{Session: session.Created.Session, MemoryID: runtimeapi.MemoryID(strings.TrimSpace(memoryID))})
	cancel()
	return err
}

func (a *App) remoteSaveDocV1(tabID, documentID, body string) (string, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return "", err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.SaveMemoryDocument(ctx, runtimeapi.SaveMemoryDocumentInput{
		Session: session.Created.Session, DocumentID: runtimeapi.DocumentID(strings.TrimSpace(documentID)), Body: body,
	})
	cancel()
	return string(result.DocumentID), err
}

func remoteResearchStatusView(view runtimeapi.ResearchStatusView) AutoResearchStatusView {
	out := AutoResearchStatusView{OpenCriteria: []AutoResearchCriterionView{}}
	if view.Task == nil {
		return out
	}
	task := view.Task
	out.TaskID = string(task.TaskID)
	out.Goal = dereferenceString(task.Goal)
	out.Status = task.Status
	out.Iteration = task.Iteration
	out.CurrentDirection = dereferenceString(task.CurrentDirection)
	out.StaleCount = task.StaleCount
	out.PivotCount = task.PivotCount
	out.PivotRequired = task.PivotRequired
	out.LastHeartbeatAt = task.LastHeartbeatAt
	out.FindingCount = task.FindingCount
	out.Blocker = dereferenceString(task.Blocker)
	out.NextRequiredAction = dereferenceString(task.NextRequiredAction)
	// TaskPath is deliberately blank: Remote V1 exposes no Desktop-operable
	// Host path and AutoResearchOpenTask remains a Desktop-local overlay.
	for _, criterion := range task.OpenCriteria {
		out.OpenCriteria = append(out.OpenCriteria, AutoResearchCriterionView{
			ID: string(criterion.CriterionID), Description: criterion.Description, Required: criterion.Required,
			EvidenceCount: criterion.EvidenceCount, Status: criterion.Status,
		})
	}
	return out
}

func (a *App) remoteResearchStatusV1(tabID string) (AutoResearchStatusView, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return AutoResearchStatusView{OpenCriteria: []AutoResearchCriterionView{}}, err
	}
	ctx, cancel := a.remoteActionContext()
	view, err := api.ResearchStatus(ctx, runtimeapi.ResearchInput{Session: session.Created.Session})
	cancel()
	return remoteResearchStatusView(view), err
}

func (a *App) remoteResearchListV1(tabID string) ([]AutoResearchStatusView, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	out := []AutoResearchStatusView{}
	for pages := 1; ; pages++ {
		page, callErr := api.ListResearch(ctx, runtimeapi.ListResearchInput{
			Session: session.Created.Session, Cursor: cursor, Limit: runtimeapi.PageMaxItems,
		})
		if callErr != nil {
			return nil, callErr
		}
		for index := range page.Items {
			out = append(out, remoteResearchStatusView(runtimeapi.ResearchStatusView{Available: true, Task: &page.Items[index]}))
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("research/list", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return nil, cursorErr
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
}

func (a *App) remoteResearchFindingsV1(tabID string, limit int) ([]AutoResearchFindingView, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	status, err := api.ResearchStatus(ctx, runtimeapi.ResearchInput{Session: session.Created.Session})
	if err != nil || status.Task == nil {
		return []AutoResearchFindingView{}, err
	}
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	out := []AutoResearchFindingView{}
	for pages := 1; ; pages++ {
		pageLimit := runtimeapi.PageMaxItems
		if limit > 0 && limit-len(out) < pageLimit {
			pageLimit = limit - len(out)
		}
		if pageLimit <= 0 {
			break
		}
		page, callErr := api.ResearchFindings(ctx, runtimeapi.ResearchFindingsInput{
			Session: session.Created.Session, TaskID: status.Task.TaskID,
			Cursor: cursor, Limit: pageLimit,
		})
		if callErr != nil {
			return nil, callErr
		}
		for _, finding := range page.Items {
			out = append(out, AutoResearchFindingView{
				ID: finding.ID, Kind: finding.Kind, Summary: dereferenceString(finding.Summary), Source: finding.Source,
				Accepted: finding.Accepted, CreatedAt: finding.CreatedAt,
			})
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("research/findings", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return nil, cursorErr
		}
		if !more || (limit > 0 && len(out) >= limit) {
			break
		}
		cursor = next
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (a *App) remoteRecordResearchEvidenceV1(tabID, criterionID string, input AutoResearchEvidenceView) error {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	status, err := api.ResearchStatus(ctx, runtimeapi.ResearchInput{Session: session.Created.Session})
	if err != nil {
		cancel()
		return err
	}
	if status.Task == nil {
		cancel()
		return errors.New("Remote Session has no active AutoResearch task")
	}
	_, err = api.RecordResearchEvidence(ctx, runtimeapi.RecordResearchEvidenceInput{
		Session: session.Created.Session, TaskID: status.Task.TaskID, CriterionID: runtimeapi.CriterionID(criterionID),
		Evidence: runtimeapi.ResearchEvidence{
			ID: input.ID, Kind: input.Kind, Summary: input.Summary, Source: input.Source,
			Command: input.Command, Paths: append([]string(nil), input.Paths...), Accepted: input.Accepted,
		},
	})
	cancel()
	return err
}

func (a *App) remoteListFilesV1(tabID, path string) ([]DirEntry, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	out := []DirEntry{}
	for pages := 1; ; pages++ {
		page, callErr := api.ListFiles(ctx, runtimeapi.FileListInput{
			Session: session.Created.Session, Path: path, Cursor: cursor, Limit: runtimeapi.PageMaxItems,
		})
		if callErr != nil {
			return nil, callErr
		}
		for _, entry := range page.Entries {
			out = append(out, DirEntry{Name: entry.Name, Path: entry.Path, IsDir: entry.IsDir})
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("file/list", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return nil, cursorErr
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
}

func (a *App) remoteSearchFilesV1(tabID, query string) ([]DirEntry, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.SearchFiles(ctx, runtimeapi.FileSearchInput{Session: session.Created.Session, Query: query, Limit: fileRefSearchLimit})
	cancel()
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, len(result.Entries))
	for index, entry := range result.Entries {
		out[index] = DirEntry{Name: entry.Path, Path: entry.Path, IsDir: entry.IsDir}
	}
	return out, nil
}

func (a *App) remoteReadFileV1(tabID, path string) FilePreview {
	out := FilePreview{Path: path}
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	ctx, cancel := a.remoteActionContext()
	preview, err := api.PreviewFile(ctx, runtimeapi.FilePreviewInput{Session: session.Created.Session, Path: path})
	cancel()
	if err != nil {
		out.Err = err.Error()
		return out
	}
	out.Path = preview.Path
	out.Size = preview.SizeBytes
	out.Truncated = preview.Truncated
	out.Binary = preview.Binary
	out.Kind = string(preview.Kind)
	if preview.Body != nil {
		out.Body = *preview.Body
	}
	return out
}

func (a *App) remoteWorkspaceChangesV1(tabID string) (WorkspaceChangesView, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return WorkspaceChangesView{Files: []WorkspaceChangeView{}, GitAvailable: false}, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	out := WorkspaceChangesView{Files: []WorkspaceChangeView{}}
	for pages := 1; ; pages++ {
		page, callErr := api.WorkspaceChanges(ctx, runtimeapi.WorkspaceChangesInput{
			Session: session.Created.Session, Cursor: cursor, Limit: runtimeapi.PageMaxItems,
		})
		if callErr != nil {
			return WorkspaceChangesView{Files: []WorkspaceChangeView{}, GitAvailable: false}, callErr
		}
		if pages == 1 {
			out.GitAvailable, out.GitBranch = page.GitAvailable, page.GitBranch
		} else if out.GitAvailable != page.GitAvailable || out.GitBranch != page.GitBranch {
			return WorkspaceChangesView{Files: []WorkspaceChangeView{}, GitAvailable: false}, errors.New("workspace/changes catalog changed while paging")
		}
		for _, file := range page.Files {
			sources := make([]string, len(file.Sources))
			for sourceIndex, source := range file.Sources {
				sources[sourceIndex] = string(source)
			}
			item := WorkspaceChangeView{
				Path: file.Path, OldPath: file.OldPath, Sources: sources, GitStatus: file.GitStatus,
				Turns: append([]int(nil), file.Turns...), LatestPrompt: file.LatestPrompt,
			}
			if file.LatestTimeMillis != nil {
				item.LatestTime = *file.LatestTimeMillis
			}
			out.Files = append(out.Files, item)
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("workspace/changes", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return WorkspaceChangesView{Files: []WorkspaceChangeView{}, GitAvailable: false}, cursorErr
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
}

func (a *App) remoteGitHistoryV1(tabID, path string) ([]GitCommitView, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.GitHistory(ctx, runtimeapi.GitHistoryInput{Session: session.Created.Session, Path: path})
	cancel()
	if err != nil {
		return nil, err
	}
	out := make([]GitCommitView, len(result.Commits))
	for index, commit := range result.Commits {
		out[index] = GitCommitView{Hash: commit.Hash, Author: commit.Author, Date: commit.Date, Message: commit.Message}
	}
	return out, nil
}

func (a *App) remoteGitCommitDetailV1(tabID, hash, path string) (GitCommitDetailView, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return GitCommitDetailView{}, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	if path != "" {
		result, callErr := api.GitCommitDetail(ctx, runtimeapi.GitCommitDetailInput{
			Session: session.Created.Session, Hash: hash, Path: path,
		})
		if callErr != nil {
			return GitCommitDetailView{}, callErr
		}
		if result.Kind != runtimeapi.GitDetailPatch {
			return GitCommitDetailView{}, errors.New("git/commitDetail returned a file page for a patch request")
		}
		return GitCommitDetailView{Diff: result.Body}, nil
	}
	out := GitCommitDetailView{Files: []string{}}
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	for pages := 1; ; pages++ {
		result, callErr := api.GitCommitDetail(ctx, runtimeapi.GitCommitDetailInput{
			Session: session.Created.Session, Hash: hash, Cursor: cursor, Limit: runtimeapi.PageMaxItems,
		})
		if callErr != nil {
			return GitCommitDetailView{}, callErr
		}
		if result.Kind != runtimeapi.GitDetailFiles || result.Files == nil || result.HasMore == nil {
			return GitCommitDetailView{}, errors.New("git/commitDetail returned an invalid file page")
		}
		for _, file := range *result.Files {
			out.Files = append(out.Files, file.Path)
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("git/commitDetail", cursor, result.Next, *result.HasMore, seen, pages)
		if cursorErr != nil {
			return GitCommitDetailView{}, cursorErr
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
}

func (a *App) remoteWorkspaceCatalogV1(tabID string) (runtimeapi.WorkspaceCatalog, runtimeapi.SessionSnapshot, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return runtimeapi.WorkspaceCatalog{}, runtimeapi.SessionSnapshot{}, err
	}
	ctx, cancel := a.remoteActionContext()
	catalog, err := api.WorkspaceCatalog(ctx, runtimeapi.WorkspaceCatalogInput{WorkspaceID: session.Created.Session.WorkspaceID})
	cancel()
	return catalog, session.Snapshot, err
}

func (a *App) remoteModelsV1(tabID string) ([]ModelInfo, error) {
	catalog, snapshot, err := a.remoteWorkspaceCatalogV1(tabID)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, len(catalog.Models))
	for index, model := range catalog.Models {
		out[index] = ModelInfo{Ref: string(model.Ref), Provider: model.Provider, Model: model.Model, Current: string(model.Ref) == snapshot.Profile.Model}
	}
	return out, nil
}

func (a *App) remoteEffortV1(tabID string) (EffortInfo, error) {
	catalog, snapshot, err := a.remoteWorkspaceCatalogV1(tabID)
	if err != nil {
		return EffortInfo{Current: "auto", Levels: []string{}}, err
	}
	for _, model := range catalog.Models {
		if string(model.Ref) == snapshot.Profile.Model {
			return EffortInfo{
				Supported: model.Effort.Supported, Current: snapshot.Profile.Effort,
				Default: model.Effort.Default, Levels: append([]string(nil), model.Effort.Levels...),
			}, nil
		}
	}
	return EffortInfo{Current: snapshot.Profile.Effort, Levels: []string{}}, nil
}

func (a *App) remoteSessionCatalogV1(tabID string) (runtimeapi.SessionCatalog, error) {
	api, session, _, _, err := a.remoteV1ForTab(tabID)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	ctx, cancel := a.remoteActionContext()
	catalog, err := api.SessionCatalog(ctx, runtimeapi.SessionCatalogInput{Session: session.Created.Session})
	cancel()
	return catalog, err
}

func (a *App) remoteCommandsV1(tabID string) ([]CommandInfo, error) {
	catalog, err := a.remoteSessionCatalogV1(tabID)
	if err != nil {
		return nil, err
	}
	out := make([]CommandInfo, len(catalog.Commands))
	for index, command := range catalog.Commands {
		out[index] = CommandInfo{Name: strings.TrimPrefix(command.Name, "/"), Description: command.Description, Kind: "custom", Group: "actions"}
	}
	return out, nil
}

func (a *App) remoteMCPServersV1(tabID string) ([]ServerView, error) {
	catalog, err := a.remoteSessionCatalogV1(tabID)
	if err != nil {
		return nil, err
	}
	return projectRemoteMCPServers(catalog.MCPServers), nil
}

func projectRemoteMCPServers(items []runtimeapi.MCPServerCatalogItem) []ServerView {
	out := make([]ServerView, len(items))
	for index, server := range items {
		status := "failed"
		if server.Available {
			status = "connected"
		}
		out[index] = ServerView{
			Name: server.Name, Status: status, Tools: server.ToolCount, HasTools: server.ToolCount > 0,
			ChangedTools: []string{}, ToolList: []ToolView{}, TrustState: "unavailable", IsolationState: "unavailable",
		}
	}
	return out
}

func (a *App) remoteSkillsV1(tabID string) (SkillsSettingsView, error) {
	catalog, err := a.remoteSessionCatalogV1(tabID)
	if err != nil {
		return SkillsSettingsView{Skills: []SkillView{}, SkillRoots: []SkillRootView{}}, err
	}
	return SkillsSettingsView{Skills: projectRemoteSkills(catalog.Skills), SkillRoots: []SkillRootView{}}, nil
}

func projectRemoteSkills(items []runtimeapi.SkillCatalogItem) []SkillView {
	out := make([]SkillView, len(items))
	for index, item := range items {
		out[index] = SkillView{
			Name: item.Name, Description: item.Description, Scope: item.Scope, RunAs: "inline", Enabled: true, Invocation: "/" + item.Name,
		}
	}
	return out
}

func (a *App) remoteCapabilitiesV1(tabID string) (CapabilitiesView, error) {
	empty := CapabilitiesView{Servers: []ServerView{}, Skills: []SkillView{}, SkillRoots: []SkillRootView{}, Plugins: []PluginView{}}
	catalog, err := a.remoteSessionCatalogV1(tabID)
	if err != nil {
		return empty, err
	}
	return CapabilitiesView{
		Servers: projectRemoteMCPServers(catalog.MCPServers), Skills: projectRemoteSkills(catalog.Skills),
		SkillRoots: []SkillRootView{}, Plugins: projectRemotePlugins(catalog.Plugins),
	}, nil
}

func (a *App) remoteListWorkspacesV1() ([]WorkspaceMeta, error) {
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	active, _, _, ok := a.remoteSessionView("")
	out := []WorkspaceMeta{}
	cursor := runtimeapi.Cursor("")
	seen := map[runtimeapi.Cursor]struct{}{cursor: {}}
	for pages := 1; ; pages++ {
		page, callErr := api.ListWorkspaces(ctx, runtimeapi.ListWorkspacesInput{Cursor: cursor, Limit: runtimeapi.PageMaxItems})
		if callErr != nil {
			return nil, callErr
		}
		for _, workspace := range page.Items {
			out = append(out, WorkspaceMeta{Path: string(workspace.ID), Name: workspace.Name, Current: ok && workspace.ID == active.Created.Session.WorkspaceID})
		}
		next, more, cursorErr := advanceRemoteLegacyCursor("workspace/list", cursor, page.Next, page.HasMore, seen, pages)
		if cursorErr != nil {
			return nil, cursorErr
		}
		if !more {
			return out, nil
		}
		cursor = next
	}
}

func (a *App) remoteRemoveWorkspaceV1(workspaceToken string) error {
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return err
	}
	id := runtimeapi.WorkspaceID(strings.TrimSpace(workspaceToken))
	if id == "" {
		return errors.New("Remote Workspace identity is required")
	}
	ctx, cancel := a.remoteActionContext()
	_, err = api.CloseWorkspace(ctx, runtimeapi.CloseWorkspaceInput{WorkspaceID: id})
	cancel()
	if err == nil {
		a.removeRemoteWorkbenchWorkspace(id)
	}
	return err
}

func (a *App) remoteSwitchWorkspaceV1(workspaceToken string) (string, error) {
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return "", err
	}
	id := runtimeapi.WorkspaceID(strings.TrimSpace(workspaceToken))
	if id == "" {
		return "", errors.New("Remote Workspace identity is required")
	}
	ctx, cancel := a.remoteActionContext()
	page, err := api.ListSessions(ctx, runtimeapi.ListSessionsInput{WorkspaceID: id, Limit: 1})
	if err != nil {
		cancel()
		return "", err
	}
	var ref runtimeapi.SessionRef
	if len(page.Items) != 0 {
		ref = page.Items[0].Session
	} else {
		created, createErr := api.CreateSession(ctx, runtimeapi.CreateSessionInput{
			WorkspaceID: id, Topic: runtimeapi.TopicSelection{Kind: runtimeapi.TopicNew},
		})
		if createErr != nil {
			cancel()
			return "", createErr
		}
		ref = created.Session
	}
	cancel()
	if _, err = a.attachRemoteWorkbenchSession(ref, true); err != nil {
		return "", err
	}
	return string(id), nil
}

// A handful of legacy bridge signatures cannot carry errors. Keep those
// failures observable without fabricating an empty success.
func logRemoteBridgeError(method string, err error) {
	if err != nil {
		// Deliberately avoid Host/session display paths in this Desktop log.
		slog.Warn("desktop: Remote bridge call failed", "method", method, "err", err)
	}
}
