package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
)

func (a *LocalTargetAdapter) workspaceRecords(scope, root string) ([]localSessionRecord, error) {
	workspaceID := localWorkspaceIDForRoot(scope, root)
	dir := desktopSessionDir(root)
	if scope == "global" {
		dir = desktopSessionDir("")
	}
	infos, err := agent.ListSessions(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	titles := loadSessionTitles(dir)
	byPath := make(map[string]localSessionRecord, len(infos))
	for _, info := range infos {
		path := sessionRuntimeKey(info.Path)
		if path == "" {
			continue
		}
		topicID := strings.TrimSpace(info.TopicID)
		if topicID == "" {
			topicID = legacySessionTopicID(info.Path)
		}
		title := strings.TrimSpace(info.CustomTitle)
		if title == "" {
			title = strings.TrimSpace(titles[filepath.Base(info.Path)])
		}
		if title == "" {
			title = strings.TrimSpace(info.TopicTitle)
		}
		if title == "" {
			title = strings.TrimSpace(info.Preview)
		}
		if title == "" {
			title = "Session"
		}
		record := localSessionRecord{
			ref:  runtimeapi.SessionRef{WorkspaceID: workspaceID, SessionID: localSessionIDForPath(workspaceID, info.Path, "")},
			path: info.Path, sessionDir: dir, workspaceRoot: root, scope: scope,
			topicID: topicID, topicTitle: info.TopicTitle, title: title, preview: info.Preview,
			turns: info.Turns, createdAt: info.CreatedAt.UnixMilli(), lastActivity: info.LastActivityAt.UnixMilli(),
		}
		byPath[path] = record
	}
	a.app.mu.RLock()
	for _, tab := range a.app.tabs {
		if tab == nil || localWorkspaceID(tab) != workspaceID {
			continue
		}
		path := sessionRuntimeKey(tab.currentSessionPath())
		ref := runtimeapi.SessionRef{WorkspaceID: workspaceID, SessionID: localSessionID(tab)}
		record, ok := byPath[path]
		if !ok {
			record = localSessionRecord{
				ref: ref, path: tab.currentSessionPath(), sessionDir: tabRuntimeSessionDir(tab),
				workspaceRoot: root, scope: scope, topicID: tab.TopicID, topicTitle: tab.TopicTitle,
				title: tab.TopicTitle, tabID: tab.ID,
			}
		} else {
			record.ref = ref
			record.tabID = tab.ID
			if strings.TrimSpace(tab.TopicTitle) != "" {
				record.topicTitle, record.title = tab.TopicTitle, tab.TopicTitle
			}
		}
		byPath[path] = record
	}
	a.app.mu.RUnlock()
	records := make([]localSessionRecord, 0, len(byPath))
	for _, record := range byPath {
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].lastActivity != records[j].lastActivity {
			return records[i].lastActivity > records[j].lastActivity
		}
		return records[i].ref.SessionID < records[j].ref.SessionID
	})
	a.mu.Lock()
	for _, record := range records {
		a.v1.records[record.ref] = record
	}
	a.mu.Unlock()
	return records, nil
}

func (a *LocalTargetAdapter) trashedWorkspaceRecords(scope, root string) ([]localSessionRecord, error) {
	workspaceID := localWorkspaceIDForRoot(scope, root)
	values := a.app.ListTrashedSessions()
	records := make([]localSessionRecord, 0)
	for _, value := range values {
		matches := false
		if scope == "global" {
			matches = strings.TrimSpace(value.Scope) == "" || value.Scope == "global"
		} else {
			matches = value.Scope != "global" && sameProjectRoot(value.WorkspaceRoot, root)
		}
		if !matches {
			continue
		}
		topicID := strings.TrimSpace(value.TopicID)
		if topicID == "" {
			topicID = legacySessionTopicID(value.Path)
		}
		title := strings.TrimSpace(value.Title)
		if title == "" {
			title = strings.TrimSpace(value.TopicTitle)
		}
		if title == "" {
			title = value.Preview
		}
		sessionDir, err := a.app.trashedSessionDir(value.Path)
		if err != nil {
			continue
		}
		record := localSessionRecord{
			ref:  runtimeapi.SessionRef{WorkspaceID: workspaceID, SessionID: localSessionIDForPath(workspaceID, value.Path, "trash")},
			path: value.Path, sessionDir: sessionDir, workspaceRoot: root, scope: scope,
			topicID: topicID, topicTitle: value.TopicTitle, title: title, preview: value.Preview,
			turns: value.Turns, createdAt: value.CreatedAt, lastActivity: value.LastActivityAt,
			deletedAt: value.DeletedAt, recoveryCopy: value.RecoveryCopy, trashed: true,
		}
		records = append(records, record)
	}
	a.mu.Lock()
	for _, record := range records {
		a.v1.records[record.ref] = record
	}
	a.mu.Unlock()
	return records, nil
}

func (a *LocalTargetAdapter) resolveRecord(ref runtimeapi.SessionRef, includeTrash bool) (localSessionRecord, error) {
	if !ref.Valid() {
		return localSessionRecord{}, errors.New("workspaceId and sessionId are required")
	}
	a.mu.Lock()
	record, ok := a.v1.records[ref]
	a.mu.Unlock()
	if ok && (includeTrash || !record.trashed) {
		return record, nil
	}
	scope, root, err := a.resolveWorkspace(ref.WorkspaceID)
	if err != nil {
		// A trashed record may be the last surviving reference to a workspace
		// removed from the sidebar. Search known trash before rejecting it.
		if includeTrash {
			a.mu.Lock()
			record, ok = a.v1.records[ref]
			a.mu.Unlock()
			if ok && record.trashed {
				return record, nil
			}
		}
		return localSessionRecord{}, err
	}
	records, err := a.workspaceRecords(scope, root)
	if err != nil {
		return localSessionRecord{}, err
	}
	if includeTrash {
		trashed, trashErr := a.trashedWorkspaceRecords(scope, root)
		if trashErr != nil {
			return localSessionRecord{}, trashErr
		}
		records = append(records, trashed...)
	}
	for _, candidate := range records {
		if candidate.ref == ref {
			return candidate, nil
		}
	}
	return localSessionRecord{}, ErrLocalSessionUnknown
}

func (a *LocalTargetAdapter) resolveTopic(workspaceID runtimeapi.WorkspaceID, topicID runtimeapi.TopicID) (string, error) {
	scope, root, err := a.resolveWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	titleRoot := root
	if scope == "global" {
		titleRoot = ""
	}
	for raw := range loadTopicTitles(titleRoot) {
		if localTopicID(workspaceID, raw) == topicID {
			return raw, nil
		}
	}
	records, _ := a.workspaceRecords(scope, root)
	for _, record := range records {
		if localTopicID(workspaceID, record.topicID) == topicID {
			return record.topicID, nil
		}
	}
	return "", errors.New("Local topic is unknown")
}

func (a *LocalTargetAdapter) ListTopics(ctx context.Context, input runtimeapi.ListTopicsInput) (runtimeapi.TopicPage, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.TopicPage{}, err
	}
	scope, root, err := a.resolveWorkspace(input.WorkspaceID)
	if err != nil {
		return runtimeapi.TopicPage{}, err
	}
	titleRoot := root
	if scope == "global" {
		titleRoot = ""
	}
	titles := loadTopicTitles(titleRoot)
	created := loadTopicCreatedAts(titleRoot)
	records, err := a.workspaceRecords(scope, root)
	if err != nil {
		return runtimeapi.TopicPage{}, err
	}
	type topicData struct {
		title             string
		created, activity int64
		count             int
	}
	byID := make(map[string]topicData)
	for raw, title := range titles {
		byID[raw] = topicData{title: title, created: created[raw]}
	}
	for _, record := range records {
		data := byID[record.topicID]
		if data.title == "" {
			data.title = record.topicTitle
		}
		if data.title == "" {
			data.title = record.title
		}
		if data.created == 0 || (record.createdAt != 0 && record.createdAt < data.created) {
			data.created = record.createdAt
		}
		if record.lastActivity > data.activity {
			data.activity = record.lastActivity
		}
		data.count++
		byID[record.topicID] = data
	}
	items := make([]runtimeapi.TopicSummary, 0, len(byID))
	for raw, data := range byID {
		if strings.TrimSpace(data.title) == "" {
			data.title = "New topic"
		}
		items = append(items, runtimeapi.TopicSummary{
			TopicID: localTopicID(input.WorkspaceID, raw), Title: data.title,
			CreatedAtMillis: data.created, SessionCount: data.count, LastActivityMillis: data.activity,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].LastActivityMillis != items[j].LastActivityMillis {
			return items[i].LastActivityMillis > items[j].LastActivityMillis
		}
		return items[i].TopicID < items[j].TopicID
	})
	revision := localHash(items)
	start, end, next, more, err := a.localPage("topic/list", string(input.WorkspaceID), revision, input.Cursor, input.Limit, len(items))
	if err != nil {
		return runtimeapi.TopicPage{}, err
	}
	return runtimeapi.TopicPage{Items: append([]runtimeapi.TopicSummary(nil), items[start:end]...), HasMore: more, Next: next}, nil
}

func (a *LocalTargetAdapter) CreateTopic(ctx context.Context, input runtimeapi.CreateTopicInput) (runtimeapi.CreatedTopic, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.CreatedTopic{}, err
	}
	defer release()
	scope, root, err := a.resolveWorkspace(input.WorkspaceID)
	if err != nil {
		return runtimeapi.CreatedTopic{}, err
	}
	if scope == "global" {
		root = ""
	}
	created, err := a.app.CreateTopic(scope, root, input.Title)
	if err != nil {
		return runtimeapi.CreatedTopic{}, err
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.WorkspaceID}, runtimeapi.CatalogTopics)
	return runtimeapi.CreatedTopic{TopicID: localTopicID(input.WorkspaceID, created.ID), Title: created.Title, CreatedAtMillis: created.CreatedAt}, nil
}

func (a *LocalTargetAdapter) RenameTopic(ctx context.Context, input runtimeapi.RenameTopicInput) (runtimeapi.RenameTopicResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.RenameTopicResult{}, err
	}
	defer release()
	raw, err := a.resolveTopic(input.WorkspaceID, input.TopicID)
	if err != nil {
		return runtimeapi.RenameTopicResult{}, err
	}
	if err := a.app.RenameTopic(raw, input.Title); err != nil {
		return runtimeapi.RenameTopicResult{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = defaultTopicTitle
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.WorkspaceID}, runtimeapi.CatalogTopics, runtimeapi.CatalogSessions)
	return runtimeapi.RenameTopicResult{Title: title}, nil
}

func (a *LocalTargetAdapter) DeleteTopic(ctx context.Context, input runtimeapi.DeleteTopicInput) (runtimeapi.DeleteTopicResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.DeleteTopicResult{}, err
	}
	defer release()
	raw, err := a.resolveTopic(input.WorkspaceID, input.TopicID)
	if err != nil {
		return runtimeapi.DeleteTopicResult{}, err
	}
	if err := a.app.DeleteTopic(raw); err != nil {
		return runtimeapi.DeleteTopicResult{}, err
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.WorkspaceID}, runtimeapi.CatalogTopics)
	return runtimeapi.DeleteTopicResult{Deleted: true}, nil
}

func (a *LocalTargetAdapter) TrashTopic(ctx context.Context, input runtimeapi.TrashTopicInput) (runtimeapi.TrashTopicResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.TrashTopicResult{}, err
	}
	defer release()
	raw, err := a.resolveTopic(input.WorkspaceID, input.TopicID)
	if err != nil {
		return runtimeapi.TrashTopicResult{}, err
	}
	scope, root, _ := a.resolveWorkspace(input.WorkspaceID)
	records, _ := a.workspaceRecords(scope, root)
	count := 0
	for _, record := range records {
		if record.topicID == raw {
			count++
		}
	}
	if err := a.app.TrashTopic(raw); err != nil {
		return runtimeapi.TrashTopicResult{}, err
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.WorkspaceID}, runtimeapi.CatalogTopics, runtimeapi.CatalogSessions, runtimeapi.CatalogTrash)
	return runtimeapi.TrashTopicResult{Disposition: runtimeapi.CleanupTrashed, TrashedSessions: count}, nil
}

func (a *LocalTargetAdapter) ListSessions(ctx context.Context, input runtimeapi.ListSessionsInput) (runtimeapi.SessionListPage, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.SessionListPage{}, err
	}
	scope, root, err := a.resolveWorkspace(input.WorkspaceID)
	if err != nil {
		return runtimeapi.SessionListPage{}, err
	}
	records, err := a.workspaceRecords(scope, root)
	if err != nil {
		return runtimeapi.SessionListPage{}, err
	}
	items := make([]runtimeapi.SessionSummary, 0, len(records))
	for _, record := range records {
		item := runtimeapi.SessionSummary{
			Session: record.ref, TopicID: localTopicID(input.WorkspaceID, record.topicID), Title: record.title,
			Preview: record.preview, Turns: record.turns, CreatedAtMillis: record.createdAt, LastActivityMillis: record.lastActivity,
		}
		if record.tabID != "" {
			if ctrl := a.app.ctrlByTabID(record.tabID); ctrl != nil {
				status := ctrl.RuntimeStatus()
				item.Runtime = &runtimeapi.SessionRuntimeSummary{Running: status.Running, PendingPrompt: status.PendingPrompt, ActiveJobs: status.BackgroundJobs}
			}
		}
		items = append(items, item)
	}
	revision := localHash(items)
	start, end, next, more, err := a.localPage("session/list", string(input.WorkspaceID), revision, input.Cursor, input.Limit, len(items))
	if err != nil {
		return runtimeapi.SessionListPage{}, err
	}
	return runtimeapi.SessionListPage{Items: append([]runtimeapi.SessionSummary(nil), items[start:end]...), HasMore: more, Next: next}, nil
}

func (a *LocalTargetAdapter) CloseSession(ctx context.Context, input runtimeapi.CloseSessionInput) (runtimeapi.CloseSessionResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.CloseSessionResult{}, err
	}
	defer release()
	a.mu.Lock()
	alreadyClosed := a.v1.closedSessions[input.Session]
	a.mu.Unlock()
	if alreadyClosed {
		return runtimeapi.CloseSessionResult{Disposition: runtimeapi.SessionAlreadyClosed}, nil
	}
	record, err := a.resolveRecord(input.Session, false)
	if err != nil {
		return runtimeapi.CloseSessionResult{}, err
	}
	if record.tabID == "" {
		return runtimeapi.CloseSessionResult{Disposition: runtimeapi.SessionAlreadyClosed}, nil
	}
	if ctrl := a.app.ctrlByTabID(record.tabID); ctrl != nil {
		status := ctrl.RuntimeStatus()
		if status.Running || status.PendingPrompt || status.BackgroundJobs > 0 {
			return runtimeapi.CloseSessionResult{Disposition: runtimeapi.SessionRetainedActive}, nil
		}
	}
	if err := a.app.CloseTab(record.tabID); err != nil {
		return runtimeapi.CloseSessionResult{}, err
	}
	a.mu.Lock()
	a.v1.closedSessions[input.Session] = true
	record.tabID = ""
	a.v1.records[input.Session] = record
	a.mu.Unlock()
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogSessions)
	return runtimeapi.CloseSessionResult{Disposition: runtimeapi.SessionReleased}, nil
}

func (a *LocalTargetAdapter) RenameSession(ctx context.Context, input runtimeapi.RenameSessionInput) (runtimeapi.RenameSessionResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.RenameSessionResult{}, err
	}
	defer release()
	record, err := a.resolveRecord(input.Session, false)
	if err != nil {
		return runtimeapi.RenameSessionResult{}, err
	}
	if err := agent.RenameSession(record.path, input.Title); err != nil {
		return runtimeapi.RenameSessionResult{}, err
	}
	if err := setSessionTitle(record.sessionDir, record.path, input.Title); err != nil {
		return runtimeapi.RenameSessionResult{}, err
	}
	a.app.invalidatePromptHistoryCache()
	a.app.emitProjectTreeChanged()
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogSessions)
	return runtimeapi.RenameSessionResult{Title: strings.TrimSpace(input.Title)}, nil
}

func (a *LocalTargetAdapter) ListTrashedSessions(ctx context.Context, input runtimeapi.ListTrashedSessionsInput) (runtimeapi.TrashPage, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.TrashPage{}, err
	}
	scope, root, err := a.resolveWorkspace(input.WorkspaceID)
	if err != nil {
		return runtimeapi.TrashPage{}, err
	}
	records, err := a.trashedWorkspaceRecords(scope, root)
	if err != nil {
		return runtimeapi.TrashPage{}, err
	}
	items := make([]runtimeapi.TrashEntry, 0, len(records))
	for _, record := range records {
		items = append(items, runtimeapi.TrashEntry{
			Session: record.ref, TopicID: localTopicID(input.WorkspaceID, record.topicID), Title: record.title,
			Preview: record.preview, TrashedAtMillis: record.deletedAt, RecoveryCopy: record.recoveryCopy,
		})
	}
	revision := localHash(items)
	start, end, next, more, err := a.localPage("session/trashList", string(input.WorkspaceID), revision, input.Cursor, input.Limit, len(items))
	if err != nil {
		return runtimeapi.TrashPage{}, err
	}
	return runtimeapi.TrashPage{Items: append([]runtimeapi.TrashEntry(nil), items[start:end]...), HasMore: more, Next: next}, nil
}

func (a *LocalTargetAdapter) TrashSession(ctx context.Context, input runtimeapi.TrashSessionInput) (runtimeapi.TrashSessionResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.TrashSessionResult{}, err
	}
	defer release()
	a.mu.Lock()
	alreadyTrashed := a.v1.trashedSessions[input.Session]
	a.mu.Unlock()
	if alreadyTrashed {
		return runtimeapi.TrashSessionResult{Disposition: runtimeapi.CleanupAlreadyTrashed}, nil
	}
	record, err := a.resolveRecord(input.Session, false)
	if err != nil {
		return runtimeapi.TrashSessionResult{}, err
	}
	if input.Guard == runtimeapi.TrashRedundantRecoveryOnly {
		err = a.app.DeleteRecoveryCopy(record.path)
	} else {
		err = a.app.DeleteSession(record.path)
	}
	if err != nil {
		return runtimeapi.TrashSessionResult{}, err
	}
	a.mu.Lock()
	a.v1.trashedSessions[input.Session] = true
	a.invalidateLocalCheckpointsLocked(input.Session)
	delete(a.v1.records, input.Session)
	a.mu.Unlock()
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogSessions, runtimeapi.CatalogTrash, runtimeapi.CatalogTopics)
	return runtimeapi.TrashSessionResult{Disposition: runtimeapi.CleanupTrashed}, nil
}

func (a *LocalTargetAdapter) RestoreSession(ctx context.Context, input runtimeapi.RestoreSessionInput) (runtimeapi.RestoreSessionResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.RestoreSessionResult{}, err
	}
	defer release()
	record, err := a.resolveRecord(input.Session, true)
	if err != nil || !record.trashed {
		return runtimeapi.RestoreSessionResult{}, ErrLocalSessionUnknown
	}
	_, key, _, err := validateTrashedSessionPath(record.sessionDir, record.path)
	if err != nil {
		return runtimeapi.RestoreSessionResult{}, err
	}
	if err := a.app.RestoreSession(record.path); err != nil {
		return runtimeapi.RestoreSessionResult{}, err
	}
	targetPath := filepath.Join(record.sessionDir, key)
	restored := runtimeapi.SessionRef{WorkspaceID: input.Session.WorkspaceID, SessionID: localSessionIDForPath(input.Session.WorkspaceID, targetPath, "")}
	a.mu.Lock()
	delete(a.v1.records, input.Session)
	delete(a.v1.trashedSessions, input.Session)
	delete(a.v1.closedSessions, restored)
	delete(a.v1.purgedSessions, input.Session)
	a.mu.Unlock()
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogSessions, runtimeapi.CatalogTrash, runtimeapi.CatalogTopics)
	return runtimeapi.RestoreSessionResult{Session: restored, TopicID: localTopicID(input.Session.WorkspaceID, record.topicID), Disposition: runtimeapi.SessionRestored}, nil
}

func (a *LocalTargetAdapter) PurgeSession(ctx context.Context, input runtimeapi.PurgeSessionInput) (runtimeapi.PurgeSessionResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.PurgeSessionResult{}, err
	}
	defer release()
	a.mu.Lock()
	alreadyPurged := a.v1.purgedSessions[input.Session]
	a.mu.Unlock()
	if alreadyPurged {
		return runtimeapi.PurgeSessionResult{Purged: false}, nil
	}
	record, err := a.resolveRecord(input.Session, true)
	if err != nil {
		return runtimeapi.PurgeSessionResult{}, err
	}
	if !record.trashed {
		return runtimeapi.PurgeSessionResult{}, errors.New("Local session is not in trash")
	}
	if input.Guard == runtimeapi.TrashRedundantRecoveryOnly {
		err = a.app.PurgeRecoveryCopy(record.path)
	} else {
		err = a.app.PurgeTrashedSession(record.path)
	}
	if err != nil {
		return runtimeapi.PurgeSessionResult{}, err
	}
	a.mu.Lock()
	a.v1.purgedSessions[input.Session] = true
	a.invalidateLocalCheckpointsLocked(input.Session)
	delete(a.v1.records, input.Session)
	a.mu.Unlock()
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogTrash, runtimeapi.CatalogTopics)
	return runtimeapi.PurgeSessionResult{Purged: true}, nil
}

func (a *LocalTargetAdapter) waitLocalTabReady(ctx context.Context, tabID string) (*WorkspaceTab, error) {
	ctx = localContext(ctx)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		a.app.mu.RLock()
		tab := a.app.tabs[tabID]
		ready := tab != nil && tab.Ready && tab.Ctrl != nil
		startupErr := ""
		if tab != nil {
			startupErr = tab.StartupErr
		}
		a.app.mu.RUnlock()
		if ready {
			return tab, nil
		}
		if strings.TrimSpace(startupErr) != "" {
			return nil, fmt.Errorf("workspace failed to start: %s", startupErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *LocalTargetAdapter) applyCreatedProfile(tabID string, profile runtimeapi.ProfileSelection) error {
	if strings.TrimSpace(profile.Model) != "" {
		if err := a.app.SetModelForTab(tabID, profile.Model); err != nil {
			return err
		}
	}
	if strings.TrimSpace(profile.Effort) != "" {
		if err := a.app.SetEffortForTab(tabID, profile.Effort); err != nil {
			return err
		}
	}
	if strings.TrimSpace(profile.CollaborationMode) != "" {
		a.app.SetCollaborationModeForTab(tabID, profile.CollaborationMode)
	}
	if strings.TrimSpace(profile.TokenMode) != "" {
		if err := a.app.SetTokenModeForTab(tabID, profile.TokenMode); err != nil {
			return err
		}
	}
	if strings.TrimSpace(profile.ToolApprovalMode) != "" {
		a.app.SetToolApprovalModeForTab(tabID, profile.ToolApprovalMode)
	}
	return nil
}

func (a *LocalTargetAdapter) CreateSession(ctx context.Context, input runtimeapi.CreateSessionInput) (runtimeapi.CreatedSession, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	defer release()
	scope, root, err := a.resolveWorkspace(input.WorkspaceID)
	if err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	rawTopic := ""
	topicTitle := strings.TrimSpace(input.Topic.Title)
	switch input.Topic.Kind {
	case runtimeapi.TopicExisting:
		rawTopic, err = a.resolveTopic(input.WorkspaceID, input.Topic.TopicID)
		if err != nil {
			return runtimeapi.CreatedSession{}, err
		}
	case runtimeapi.TopicNew:
		created, createErr := a.app.CreateTopic(scope, root, topicTitle)
		if createErr != nil {
			return runtimeapi.CreatedSession{}, createErr
		}
		rawTopic, topicTitle = created.ID, created.Title
	default:
		return runtimeapi.CreatedSession{}, errors.New("topic selection kind is required")
	}
	var meta TabMeta
	if scope == "project" {
		meta, err = a.app.OpenProjectTab(root, rawTopic)
	} else {
		meta, err = a.app.OpenGlobalTab(rawTopic)
	}
	if err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	tab, err := a.waitLocalTabReady(ctx, meta.ID)
	if err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	if err := a.applyCreatedProfile(tab.ID, input.Profile); err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	if len(input.AdditionalDirectories) > 0 {
		ctrl := a.app.controllerForTab(tab)
		for _, ref := range input.AdditionalDirectories {
			a.mu.Lock()
			path := a.v1.directories[ref]
			a.mu.Unlock()
			if path == "" {
				return runtimeapi.CreatedSession{}, errors.New("additional directory reference is unknown")
			}
			if _, _, err := ctrl.RegisterExternalFolderRef(path); err != nil {
				return runtimeapi.CreatedSession{}, err
			}
		}
	}
	a.mu.Lock()
	a.refreshSessionsLocked()
	ref := a.tabSessions[tab.ID]
	a.mu.Unlock()
	if !ref.Valid() {
		return runtimeapi.CreatedSession{}, ErrLocalSessionUnknown
	}
	if topicTitle == "" {
		topicTitle = tab.TopicTitle
	}
	resolved := runtimeapi.ResolvedProfile{
		Model: tab.model, CollaborationMode: currentTabCollaborationMode(tab), TokenMode: currentTabTokenMode(tab), ToolApprovalMode: currentTabToolApprovalMode(tab),
	}
	if tab.effort != nil {
		resolved.Effort = *tab.effort
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.WorkspaceID}, runtimeapi.CatalogSessions, runtimeapi.CatalogTopics)
	return runtimeapi.CreatedSession{Session: ref, TopicID: localTopicID(input.WorkspaceID, rawTopic), TopicTitle: topicTitle, ResolvedProfile: resolved}, nil
}

func (a *LocalTargetAdapter) ensureSessionOpen(ctx context.Context, ref runtimeapi.SessionRef) (runtimeapi.SessionRef, error) {
	a.mu.Lock()
	a.refreshSessionsLocked()
	if state := a.sessions[ref]; state != nil {
		a.mu.Unlock()
		return ref, nil
	}
	a.mu.Unlock()
	record, err := a.resolveRecord(ref, false)
	if err != nil {
		return runtimeapi.SessionRef{}, err
	}
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.SessionRef{}, err
	}
	defer release()
	meta, err := a.app.OpenTopicSession(record.scope, record.workspaceRoot, record.topicID, record.path)
	if err != nil {
		return runtimeapi.SessionRef{}, err
	}
	if _, err := a.waitLocalTabReady(ctx, meta.ID); err != nil {
		return runtimeapi.SessionRef{}, err
	}
	a.mu.Lock()
	a.refreshSessionsLocked()
	opened := a.tabSessions[meta.ID]
	a.mu.Unlock()
	if opened != ref {
		return runtimeapi.SessionRef{}, errors.New("Local session identity changed while opening")
	}
	return opened, nil
}

func (a *LocalTargetAdapter) UnsubscribeSession(ctx context.Context, input runtimeapi.UnsubscribeSessionInput) error {
	if err := localCheckContext(ctx); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, _, err := a.sessionLocked(input.Session)
	if err != nil {
		return err
	}
	state.subscribed = false
	return nil
}

func (a *LocalTargetAdapter) SessionHistory(ctx context.Context, input runtimeapi.HistoryInput) (runtimeapi.HistoryPage, error) {
	if err := runtimeapi.ValidateHistoryTurns(input.PageTurns); err != nil {
		return runtimeapi.HistoryPage{}, err
	}
	tab, _, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.HistoryPage{}, err
	}
	defer a.endLocalSession()
	before := 0
	if input.Cursor != "" {
		a.mu.Lock()
		cursor, ok := a.v1.cursors[input.Cursor]
		a.mu.Unlock()
		if !ok || cursor.kind != "session/history" || cursor.binding != string(input.Session.SessionID) {
			return runtimeapi.HistoryPage{}, runtimeservice.ErrStaleCursor
		}
		current := a.app.HistoryPageForTab(tab.ID, 0, 1)
		if cursor.revision != fmt.Sprint(current.TotalTurns) {
			return runtimeapi.HistoryPage{}, runtimeservice.ErrStaleCursor
		}
		before = cursor.offset
	}
	page := a.app.HistoryPageForTab(tab.ID, before, input.PageTurns)
	checkpointValues := a.app.CheckpointsForTab(tab.ID)
	a.mu.Lock()
	checkpointIDs, checkpointErr := a.syncLocalCheckpointsLocked(input.Session, checkpointValues)
	a.mu.Unlock()
	if checkpointErr != nil {
		return runtimeapi.HistoryPage{}, checkpointErr
	}
	result := projectLocalHistory(page, checkpointIDs)
	if result.HasOlder {
		id, idErr := newLocalOpaqueID("local_history_cursor")
		if idErr != nil {
			return runtimeapi.HistoryPage{}, idErr
		}
		result.Next = runtimeapi.Cursor(id)
		a.mu.Lock()
		a.v1.cursors[result.Next] = localRuntimeCursor{kind: "session/history", binding: string(input.Session.SessionID), revision: fmt.Sprint(result.TotalTurns), offset: result.StartTurn}
		a.mu.Unlock()
	}
	return result, nil
}

func (a *LocalTargetAdapter) SessionContent(ctx context.Context, input runtimeapi.ContentInput) (runtimeapi.ContentChunk, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.ContentChunk{}, err
	}
	if strings.TrimSpace(string(input.ContentRef)) == "" || input.Offset < 0 {
		return runtimeapi.ContentChunk{}, errors.New("contentRef and a non-negative offset are required")
	}
	return runtimeapi.ContentChunk{}, runtimeapi.Unavailable(runtimeapi.CapabilitySessionAttach, "Local history is delivered inline and never issues contentRef capabilities")
}

func localArgData(a *App, tab *WorkspaceTab, ctrl control.SessionAPI) control.ArgData {
	data := control.ArgData{
		Skills: ctrl.Skills(), DisabledSkills: ctrl.DisabledSkills(), ConfiguredMCP: ctrl.ConfiguredMCPNames(),
		DisconnectedMCP: ctrl.DisconnectedMCPNames(), CurrentModel: tab.model,
	}
	if names, err := pluginpkg.InstalledNames(config.ReasonixHomeDir()); err == nil {
		data.PluginNames = names
	}
	seenProviders := make(map[string]bool)
	for _, model := range a.ModelsForTab(tab.ID) {
		data.ModelRefs = append(data.ModelRefs, model.Ref)
		if model.Provider != "" && !seenProviders[model.Provider] {
			seenProviders[model.Provider] = true
			data.ProviderNames = append(data.ProviderNames, model.Provider)
		}
		if model.Current {
			data.CurrentProvider = model.Provider
		}
	}
	if host := ctrl.Host(); host != nil {
		data.ServerNames = host.ServerNames()
	}
	return data
}

func (a *LocalTargetAdapter) ComposerSlashArgs(ctx context.Context, input runtimeapi.SlashArgsInput) (runtimeapi.SlashArgsResult, error) {
	tab, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.SlashArgsResult{}, err
	}
	defer a.endLocalSession()
	return runtimeservice.ProjectSlashArgs(input.Input, localArgData(a.app, tab, ctrl))
}

func (a *LocalTargetAdapter) ComposerHistory(ctx context.Context, input runtimeapi.PromptHistoryInput) (runtimeapi.PromptHistoryPage, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.PromptHistoryPage{}, err
	}
	if a.v1.promptHistoryErr != nil || a.v1.promptHistory == nil {
		return runtimeapi.PromptHistoryPage{}, a.v1.promptHistoryErr
	}
	scope, root, err := a.resolveWorkspace(input.WorkspaceID)
	if err != nil {
		return runtimeapi.PromptHistoryPage{}, err
	}
	records, err := a.workspaceRecords(scope, root)
	if err != nil {
		return runtimeapi.PromptHistoryPage{}, err
	}
	sources := make([]runtimeservice.PromptHistorySessionSource, 0, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.path) == "" {
			continue
		}
		if info, statErr := os.Stat(record.path); statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		sources = append(sources, runtimeservice.PromptHistorySessionSource{Session: record.ref, SessionDir: record.sessionDir, SessionPath: record.path})
	}
	return a.v1.promptHistory.History(localContext(ctx), input, sources)
}
