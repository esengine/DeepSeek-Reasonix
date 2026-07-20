package catalog

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/store"
)

func (c *Catalog) ListTopics(ctx context.Context, params protocol.TopicListParams) (protocol.TopicListResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.TopicListResult{}, err
	}
	limit, err := normalizedLimit(params.Limit)
	if err != nil {
		return protocol.TopicListResult{}, err
	}
	workspace, err := c.openWorkspaceLocked(params.WorkspaceID)
	if err != nil {
		return protocol.TopicListResult{}, err
	}
	if err := c.syncWorkspaceSessionsLocked(ctx, workspace); err != nil {
		return protocol.TopicListResult{}, err
	}

	items := make([]protocol.TopicSummary, 0, len(c.state.Topics[params.WorkspaceID]))
	for _, topic := range c.state.Topics[params.WorkspaceID] {
		if topic.Trashed {
			continue
		}
		item := protocol.TopicSummary{
			TopicID:          topic.ID,
			Title:            topic.Title,
			CreatedAtMs:      topic.CreatedAtMs,
			LastActivityAtMs: topic.CreatedAtMs,
		}
		for _, session := range c.state.Sessions {
			if session.WorkspaceID != params.WorkspaceID || session.TopicID != topic.ID || session.TrashPath != "" {
				continue
			}
			if _, err := c.validateLiveSessionRecordLocked(workspace, session); err != nil {
				return protocol.TopicListResult{}, err
			}
			if err := validateMetaSidecar(session.Path, true); err != nil {
				return protocol.TopicListResult{}, err
			}
			meta, ok, loadErr := agent.LoadBranchMeta(session.Path)
			if loadErr != nil || !ok {
				if loadErr == nil {
					loadErr = errors.New("Session sidecar is missing")
				}
				return protocol.TopicListResult{}, catalogError(protocol.ErrSessionPersistFailed, loadErr)
			}
			item.SessionCount++
			_, activity := metadataTimes(meta, c.now().UTC())
			if millis := nonnegativeUnixMilli(activity); millis > item.LastActivityAtMs {
				item.LastActivityAtMs = millis
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastActivityAtMs != items[j].LastActivityAtMs {
			return items[i].LastActivityAtMs > items[j].LastActivityAtMs
		}
		if items[i].CreatedAtMs != items[j].CreatedAtMs {
			return items[i].CreatedAtMs < items[j].CreatedAtMs
		}
		return items[i].TopicID < items[j].TopicID
	})
	start, err := c.pageStartLocked("topic/list", string(params.WorkspaceID), params.Cursor, c.state.Revision)
	if err != nil {
		return protocol.TopicListResult{}, err
	}
	end := minInt(start+limit, len(items))
	page := make([]protocol.TopicSummary, end-start)
	copy(page, items[start:end])
	hasMore := end < len(items)
	var next protocol.Cursor
	if hasMore {
		next, err = c.storeCursorLocked(cursorRecord{Method: "topic/list", Binding: string(params.WorkspaceID), Revision: c.state.Revision, Offset: end})
		if err != nil {
			return protocol.TopicListResult{}, err
		}
	}
	return protocol.TopicListResult{Items: page, HasMore: hasMore, NextCursor: next}, nil
}

func (c *Catalog) CreateTopic(params protocol.TopicCreateParams) (protocol.TopicCreateResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.TopicCreateResult{}, err
	}
	if _, err := c.openWorkspaceLocked(params.WorkspaceID); err != nil {
		return protocol.TopicCreateResult{}, err
	}
	raw, err := c.nextIDLocked("topic")
	if err != nil {
		return protocol.TopicCreateResult{}, err
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		title = c.defaultTopicTitle
	}
	created := c.now().UTC().UnixMilli()
	if created < 0 {
		created = 0
	}
	record := topicRecord{ID: protocol.TopicID(raw), Title: title, CreatedAtMs: created}
	if err := c.mutateLocked(func() error {
		if c.state.Topics[params.WorkspaceID] == nil {
			c.state.Topics[params.WorkspaceID] = make(map[protocol.TopicID]topicRecord)
		}
		c.state.Topics[params.WorkspaceID][record.ID] = record
		return nil
	}); err != nil {
		return protocol.TopicCreateResult{}, err
	}
	return protocol.TopicCreateResult{TopicID: record.ID, Title: record.Title, CreatedAtMs: record.CreatedAtMs, SessionCount: 0}, nil
}

func (c *Catalog) RenameTopic(params protocol.TopicRenameParams) (protocol.TopicRenameResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.TopicRenameResult{}, err
	}
	if _, err := c.openWorkspaceLocked(params.WorkspaceID); err != nil {
		return protocol.TopicRenameResult{}, err
	}
	topic, ok := c.state.Topics[params.WorkspaceID][params.TopicID]
	if !ok || topic.Trashed {
		return protocol.TopicRenameResult{}, catalogError(protocol.ErrTopicNotFound, errors.New("unknown Topic identity"))
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		return protocol.TopicRenameResult{}, errors.New("catalog: Topic title is empty")
	}
	if title == topic.Title {
		return protocol.TopicRenameResult{Title: title}, nil
	}
	backups, err := c.rewriteTopicSidecarsLocked(params.WorkspaceID, params.TopicID, title)
	if err != nil {
		return protocol.TopicRenameResult{}, err
	}
	previous := topic.Title
	topic.Title = title
	if err := c.mutateLocked(func() error {
		c.state.Topics[params.WorkspaceID][params.TopicID] = topic
		return nil
	}); err != nil {
		c.restoreSidecarBackups(backups)
		return protocol.TopicRenameResult{}, err
	}
	_ = previous // retained in backups for rollback; registry rollback is mutateLocked's responsibility.
	return protocol.TopicRenameResult{Title: title}, nil
}

func (c *Catalog) DeleteTopic(params protocol.TopicDeleteParams) (protocol.TopicDeleteResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.TopicDeleteResult{}, err
	}
	if _, err := c.openWorkspaceLocked(params.WorkspaceID); err != nil {
		return protocol.TopicDeleteResult{}, err
	}
	topic, ok := c.state.Topics[params.WorkspaceID][params.TopicID]
	if !ok || topic.Trashed {
		return protocol.TopicDeleteResult{}, catalogError(protocol.ErrTopicNotFound, errors.New("unknown Topic identity"))
	}
	for _, session := range c.state.Sessions {
		if session.WorkspaceID == params.WorkspaceID && session.TopicID == params.TopicID {
			return protocol.TopicDeleteResult{}, catalogError(protocol.ErrTopicNotEmpty, errors.New("Topic still owns Sessions or trash entries"))
		}
	}
	if err := c.mutateLocked(func() error {
		delete(c.state.Topics[params.WorkspaceID], params.TopicID)
		return nil
	}); err != nil {
		return protocol.TopicDeleteResult{}, err
	}
	return protocol.TopicDeleteResult{Deleted: true}, nil
}

// TrashTopic is the cold catalog transition used after RuntimeManager has
// quiesced every member target. It still independently takes each Session's
// cross-process removal guard, so a stale or foreign runtime cannot be raced.
func (c *Catalog) TrashTopic(params protocol.TopicTrashParams) (protocol.TopicTrashResult, error) {
	return c.trashTopic(params, false)
}

// TrashTopicReserved is used only while RuntimeManager has terminated every
// member runtime and sealed GetOrCreate for those targets.
func (c *Catalog) TrashTopicReserved(params protocol.TopicTrashParams) (protocol.TopicTrashResult, error) {
	return c.trashTopic(params, true)
}

func (c *Catalog) trashTopic(params protocol.TopicTrashParams, runtimeReserved bool) (protocol.TopicTrashResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.TopicTrashResult{}, err
	}
	workspace, err := c.openWorkspaceLocked(params.WorkspaceID)
	if err != nil {
		return protocol.TopicTrashResult{}, err
	}
	topic, ok := c.state.Topics[params.WorkspaceID][params.TopicID]
	if !ok || topic.Trashed {
		return protocol.TopicTrashResult{}, catalogError(protocol.ErrTopicNotFound, errors.New("unknown Topic identity"))
	}
	records := c.liveTopicSessionsLocked(params.WorkspaceID, params.TopicID)
	guards, err := c.acquireColdGuardsLocked(records, runtimeReserved)
	if err != nil {
		return protocol.TopicTrashResult{}, err
	}
	defer releaseRemovalGuards(guards)

	moves := make([]artifactMove, 0)
	nextRecords := make(map[protocol.SessionID]sessionRecord, len(records))
	for _, record := range records {
		trashPath, moved, moveErr := c.moveToTrashLocked(workspace, record)
		if moveErr != nil {
			rollbackArtifactMoves(moves)
			removeEmptyMoveDestinations(moves)
			return protocol.TopicTrashResult{}, moveErr
		}
		moves = append(moves, moved...)
		record.TrashPath = trashPath
		record.TrashedAtMs = c.now().UTC().UnixMilli()
		if record.TrashedAtMs < 0 {
			record.TrashedAtMs = 0
		}
		nextRecords[record.ID] = record
	}
	topic.Trashed = true
	topic.TrashedAtMs = c.now().UTC().UnixMilli()
	if topic.TrashedAtMs < 0 {
		topic.TrashedAtMs = 0
	}
	if err := c.mutateLocked(func() error {
		for id, record := range nextRecords {
			c.state.Sessions[id] = record
		}
		c.state.Topics[params.WorkspaceID][params.TopicID] = topic
		return nil
	}); err != nil {
		rollbackArtifactMoves(moves)
		removeEmptyMoveDestinations(moves)
		return protocol.TopicTrashResult{}, err
	}
	disposition := protocol.DispositionTrashed
	for _, guard := range guards {
		if err := guard.RemoveSidecarsAndRelease(); err != nil {
			disposition = protocol.DispositionCleanupPending
		}
	}
	return protocol.TopicTrashResult{Disposition: disposition, TrashedSessions: len(records)}, nil
}

func (c *Catalog) RenameSession(params protocol.SessionRenameParams) (protocol.SessionRenameResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.SessionRenameResult{}, err
	}
	record, _, err := c.liveRecordLocked(params.Target)
	if err != nil {
		return protocol.SessionRenameResult{}, err
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		return protocol.SessionRenameResult{}, errors.New("catalog: Session title is empty")
	}
	meta, ok, err := agent.LoadBranchMeta(record.Path)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("Session sidecar is missing")
		}
		return protocol.SessionRenameResult{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if meta.CustomTitle == title {
		return protocol.SessionRenameResult{Title: title}, nil
	}
	oldTitle := meta.CustomTitle
	if _, err := agent.UpdateBranchMetaPreserveUpdated(record.Path, func(current *agent.BranchMeta) error {
		current.CustomTitle = title
		return nil
	}); err != nil {
		return protocol.SessionRenameResult{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if err := c.mutateLocked(func() error { return nil }); err != nil {
		_, _ = agent.UpdateBranchMetaPreserveUpdated(record.Path, func(current *agent.BranchMeta) error {
			current.CustomTitle = oldTitle
			return nil
		})
		return protocol.SessionRenameResult{}, err
	}
	return protocol.SessionRenameResult{Title: title}, nil
}

func (c *Catalog) ListTrash(ctx context.Context, params protocol.SessionTrashListParams) (protocol.SessionTrashListResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.SessionTrashListResult{}, err
	}
	limit, err := normalizedLimit(params.Limit)
	if err != nil {
		return protocol.SessionTrashListResult{}, err
	}
	workspace, err := c.openWorkspaceLocked(params.WorkspaceID)
	if err != nil {
		return protocol.SessionTrashListResult{}, err
	}
	if err := c.syncWorkspaceSessionsLocked(ctx, workspace); err != nil {
		return protocol.SessionTrashListResult{}, err
	}
	records := make([]sessionRecord, 0)
	for _, record := range c.state.Sessions {
		if record.WorkspaceID == params.WorkspaceID && record.TrashPath != "" {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].TrashedAtMs != records[j].TrashedAtMs {
			return records[i].TrashedAtMs > records[j].TrashedAtMs
		}
		return records[i].ID < records[j].ID
	})
	start, err := c.pageStartLocked("session/trashList", string(params.WorkspaceID), params.Cursor, c.state.Revision)
	if err != nil {
		return protocol.SessionTrashListResult{}, err
	}
	end := minInt(start+limit, len(records))
	items := make([]protocol.TrashEntry, 0, end-start)
	for _, record := range records[start:end] {
		if err := c.validateTrashPathLocked(workspace, record); err != nil {
			return protocol.SessionTrashListResult{}, err
		}
		if err := validateMetaSidecar(record.TrashPath, true); err != nil {
			return protocol.SessionTrashListResult{}, err
		}
		meta, ok, loadErr := agent.LoadBranchMeta(record.TrashPath)
		if loadErr != nil || !ok {
			if loadErr == nil {
				loadErr = errors.New("trash sidecar is missing")
			}
			return protocol.SessionTrashListResult{}, catalogError(protocol.ErrSessionPersistFailed, loadErr)
		}
		topic := c.state.Topics[params.WorkspaceID][record.TopicID]
		items = append(items, protocol.TrashEntry{
			Target:       protocol.RuntimeTarget{WorkspaceID: params.WorkspaceID, SessionID: record.ID},
			TopicID:      record.TopicID,
			Title:        sessionTitle(meta, topic.Title),
			Preview:      meta.Preview,
			TrashedAtMs:  record.TrashedAtMs,
			RecoveryCopy: agent.RecoveryBranchCoveredByParent(record.TrashPath, filepath.Dir(record.Path)),
		})
	}
	hasMore := end < len(records)
	var next protocol.Cursor
	if hasMore {
		next, err = c.storeCursorLocked(cursorRecord{Method: "session/trashList", Binding: string(params.WorkspaceID), Revision: c.state.Revision, Offset: end})
		if err != nil {
			return protocol.SessionTrashListResult{}, err
		}
	}
	return protocol.SessionTrashListResult{Items: items, HasMore: hasMore, NextCursor: next}, nil
}

func (c *Catalog) TrashSession(params protocol.SessionTrashParams) (protocol.SessionTrashResult, error) {
	return c.trashSession(params, false)
}

// TrashSessionReserved is the cold transition paired with a
// RuntimeManager TargetRemovalReservation.
func (c *Catalog) TrashSessionReserved(params protocol.SessionTrashParams) (protocol.SessionTrashResult, error) {
	return c.trashSession(params, true)
}

func (c *Catalog) trashSession(params protocol.SessionTrashParams, runtimeReserved bool) (protocol.SessionTrashResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.SessionTrashResult{}, err
	}
	record, workspace, err := c.recordLocked(params.Target)
	if err != nil {
		return protocol.SessionTrashResult{}, err
	}
	if record.TrashPath != "" {
		return protocol.SessionTrashResult{Disposition: protocol.DispositionAlreadyTrashed}, nil
	}
	parentGuard, err := c.acquireRecoveryGuardLocked(record, params.Guard)
	if err != nil {
		return protocol.SessionTrashResult{}, err
	}
	if parentGuard != nil {
		defer parentGuard.Release()
	}
	guards, err := c.acquireColdGuardsLocked([]sessionRecord{record}, runtimeReserved)
	if err != nil {
		return protocol.SessionTrashResult{}, err
	}
	guard := guards[0]
	defer guard.Release()
	trashPath, moves, err := c.moveToTrashLocked(workspace, record)
	if err != nil {
		return protocol.SessionTrashResult{}, err
	}
	record.TrashPath = trashPath
	record.TrashedAtMs = c.now().UTC().UnixMilli()
	if record.TrashedAtMs < 0 {
		record.TrashedAtMs = 0
	}
	if err := c.mutateLocked(func() error {
		c.state.Sessions[record.ID] = record
		return nil
	}); err != nil {
		rollbackArtifactMoves(moves)
		removeEmptyMoveDestinations(moves)
		return protocol.SessionTrashResult{}, err
	}
	disposition := protocol.DispositionTrashed
	if err := guard.RemoveSidecarsAndRelease(); err != nil {
		disposition = protocol.DispositionCleanupPending
	}
	return protocol.SessionTrashResult{Disposition: disposition}, nil
}

func (c *Catalog) RestoreSession(params protocol.SessionRestoreParams) (protocol.SessionRestoreResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.SessionRestoreResult{}, err
	}
	record, workspace, err := c.recordLocked(params.Target)
	if err != nil {
		return protocol.SessionRestoreResult{}, err
	}
	if record.TrashPath == "" {
		return protocol.SessionRestoreResult{}, catalogError(protocol.ErrTrashEntryNotFound, errors.New("Session is not in trash"))
	}
	if err := c.validateTrashPathLocked(workspace, record); err != nil {
		return protocol.SessionRestoreResult{}, err
	}
	destination, err := c.restoreDestinationLocked(workspace, record)
	if err != nil {
		return protocol.SessionRestoreResult{}, err
	}
	guard, err := agent.TryAcquireSessionRemovalGuard(destination)
	if err != nil {
		return protocol.SessionRestoreResult{}, coldGuardError(err)
	}
	defer guard.Release()
	moves, err := moveArtifactSet(record.TrashPath, destination)
	if err != nil {
		return protocol.SessionRestoreResult{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	oldLocalID := agent.BranchID(record.TrashPath)
	if _, err := agent.UpdateBranchMetaPreserveUpdated(destination, func(meta *agent.BranchMeta) error {
		if protocol.SessionID(meta.RemoteSessionID) != record.ID {
			return errors.New("trash sidecar identity does not match registry")
		}
		meta.ID = agent.BranchID(destination)
		return nil
	}); err != nil {
		rollbackArtifactMoves(moves)
		return protocol.SessionRestoreResult{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	oldTrash := record.TrashPath
	record.Path = destination
	record.TrashPath = ""
	record.TrashedAtMs = 0
	topic := c.state.Topics[record.WorkspaceID][record.TopicID]
	topic.Trashed = false
	topic.TrashedAtMs = 0
	if err := c.mutateLocked(func() error {
		c.state.Sessions[record.ID] = record
		c.state.Topics[record.WorkspaceID][record.TopicID] = topic
		return nil
	}); err != nil {
		_, _ = agent.UpdateBranchMetaPreserveUpdated(destination, func(meta *agent.BranchMeta) error {
			meta.ID = oldLocalID
			return nil
		})
		rollbackArtifactMoves(moves)
		return protocol.SessionRestoreResult{}, err
	}
	_ = guard.RemoveSidecarsAndRelease()
	_ = os.Remove(filepath.Dir(oldTrash))
	return protocol.SessionRestoreResult{
		Target:      params.Target,
		TopicID:     record.TopicID,
		Disposition: protocol.SessionRestored,
	}, nil
}

func (c *Catalog) PurgeSession(params protocol.SessionPurgeParams) (protocol.SessionPurgeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.SessionPurgeResult{}, err
	}
	record, workspace, err := c.recordLocked(params.Target)
	if err != nil {
		return protocol.SessionPurgeResult{}, err
	}
	if record.TrashPath == "" {
		return protocol.SessionPurgeResult{}, catalogError(protocol.ErrTrashEntryNotFound, errors.New("Session is not in trash"))
	}
	if err := c.validateTrashPathLocked(workspace, record); err != nil {
		return protocol.SessionPurgeResult{}, err
	}
	parentGuard, err := c.acquireRecoveryGuardLocked(record, params.Guard)
	if err != nil {
		return protocol.SessionPurgeResult{}, err
	}
	if parentGuard != nil {
		defer parentGuard.Release()
	}
	container := filepath.Dir(record.TrashPath)
	staged := container + ".purging"
	if _, err := os.Lstat(staged); err == nil {
		return protocol.SessionPurgeResult{}, catalogError(protocol.ErrSessionCleanupPending, errors.New("a previous purge cleanup is pending"))
	} else if !os.IsNotExist(err) {
		return protocol.SessionPurgeResult{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if err := os.Rename(container, staged); err != nil {
		return protocol.SessionPurgeResult{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	topicID := record.TopicID
	if err := c.mutateLocked(func() error {
		delete(c.state.Sessions, record.ID)
		c.state.RetiredSessionIDs[record.ID] = true
		if topic := c.state.Topics[record.WorkspaceID][topicID]; topic.Trashed && !c.topicHasSessionsLocked(record.WorkspaceID, topicID) {
			delete(c.state.Topics[record.WorkspaceID], topicID)
		}
		return nil
	}); err != nil {
		_ = os.Rename(staged, container)
		return protocol.SessionPurgeResult{}, err
	}
	if err := c.removeAll(staged); err != nil {
		return protocol.SessionPurgeResult{}, catalogError(protocol.ErrSessionCleanupPending, err)
	}
	return protocol.SessionPurgeResult{Purged: true}, nil
}

type sidecarBackup struct {
	path string
	meta agent.BranchMeta
}

func (c *Catalog) rewriteTopicSidecarsLocked(workspaceID protocol.WorkspaceID, topicID protocol.TopicID, title string) ([]sidecarBackup, error) {
	backups := make([]sidecarBackup, 0)
	workspace, ok := c.state.Workspaces[workspaceID]
	if !ok {
		return nil, catalogError(protocol.ErrWorkspaceNotFound, errors.New("unknown workspace identity"))
	}
	for _, record := range c.state.Sessions {
		if record.WorkspaceID != workspaceID || record.TopicID != topicID {
			continue
		}
		path := record.Path
		if record.TrashPath != "" {
			if err := c.validateTrashPathLocked(workspace, record); err != nil {
				c.restoreSidecarBackups(backups)
				return nil, err
			}
			path = record.TrashPath
		} else if _, err := c.validateLiveSessionRecordLocked(workspace, record); err != nil {
			c.restoreSidecarBackups(backups)
			return nil, err
		}
		if err := validateMetaSidecar(path, true); err != nil {
			c.restoreSidecarBackups(backups)
			return nil, err
		}
		meta, ok, err := agent.LoadBranchMeta(path)
		if err != nil || !ok {
			if err == nil {
				err = errors.New("Session sidecar is missing")
			}
			c.restoreSidecarBackups(backups)
			return nil, catalogError(protocol.ErrSessionPersistFailed, err)
		}
		backups = append(backups, sidecarBackup{path: path, meta: meta})
		if _, err := agent.UpdateBranchMetaPreserveUpdated(path, func(current *agent.BranchMeta) error {
			current.TopicTitle = title
			return nil
		}); err != nil {
			c.restoreSidecarBackups(backups)
			return nil, catalogError(protocol.ErrSessionPersistFailed, err)
		}
	}
	return backups, nil
}

func (c *Catalog) restoreSidecarBackups(backups []sidecarBackup) {
	for i := len(backups) - 1; i >= 0; i-- {
		backup := backups[i]
		_, _ = agent.UpdateBranchMetaPreserveUpdated(backup.path, func(current *agent.BranchMeta) error {
			current.TopicTitle = backup.meta.TopicTitle
			return nil
		})
	}
}

func (c *Catalog) openWorkspaceLocked(id protocol.WorkspaceID) (workspaceRecord, error) {
	workspace, ok := c.state.Workspaces[id]
	if !ok || !workspace.Open {
		return workspaceRecord{}, catalogError(protocol.ErrWorkspaceNotFound, errors.New("workspace is not open"))
	}
	return workspace, nil
}

func (c *Catalog) recordLocked(target protocol.RuntimeTarget) (sessionRecord, workspaceRecord, error) {
	workspace, err := c.openWorkspaceLocked(target.WorkspaceID)
	if err != nil {
		return sessionRecord{}, workspaceRecord{}, err
	}
	record, ok := c.state.Sessions[target.SessionID]
	if !ok {
		return sessionRecord{}, workspaceRecord{}, catalogError(protocol.ErrSessionNotFound, errors.New("unknown Session identity"))
	}
	if record.WorkspaceID != target.WorkspaceID {
		return sessionRecord{}, workspaceRecord{}, catalogError(protocol.ErrWorkspaceSessionMismatch, errors.New("Session belongs to another workspace"))
	}
	return record, workspace, nil
}

func (c *Catalog) liveRecordLocked(target protocol.RuntimeTarget) (sessionRecord, workspaceRecord, error) {
	record, workspace, err := c.recordLocked(target)
	if err != nil {
		return sessionRecord{}, workspaceRecord{}, err
	}
	if record.TrashPath != "" {
		return sessionRecord{}, workspaceRecord{}, catalogError(protocol.ErrSessionTrashed, errors.New("Session is in trash"))
	}
	if _, err := c.validateLiveSessionRecordLocked(workspace, record); err != nil {
		return sessionRecord{}, workspaceRecord{}, err
	}
	return record, workspace, nil
}

func (c *Catalog) liveTopicSessionsLocked(workspaceID protocol.WorkspaceID, topicID protocol.TopicID) []sessionRecord {
	records := make([]sessionRecord, 0)
	for _, record := range c.state.Sessions {
		if record.WorkspaceID == workspaceID && record.TopicID == topicID && record.TrashPath == "" {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	return records
}

func (c *Catalog) acquireColdGuardsLocked(records []sessionRecord, runtimeReserved bool) ([]*agent.SessionRemovalGuard, error) {
	guards := make([]*agent.SessionRemovalGuard, 0, len(records))
	for _, record := range records {
		target := protocol.RuntimeTarget{WorkspaceID: record.WorkspaceID, SessionID: record.ID}
		if c.runtimeInspector != nil && !runtimeReserved {
			if _, exists := c.runtimeInspector.SessionSummary(target); exists {
				releaseRemovalGuards(guards)
				return nil, catalogError(protocol.ErrSessionBusy, errors.New("Session runtime must be released before cold mutation"))
			}
		}
		guard, err := agent.TryAcquireSessionRemovalGuard(record.Path)
		if err != nil {
			releaseRemovalGuards(guards)
			return nil, coldGuardError(err)
		}
		guards = append(guards, guard)
	}
	return guards, nil
}

func coldGuardError(err error) error {
	if errors.Is(err, agent.ErrSessionLeaseHeld) {
		return catalogError(protocol.ErrSessionBusy, err)
	}
	return catalogError(protocol.ErrSessionPersistFailed, err)
}

func releaseRemovalGuards(guards []*agent.SessionRemovalGuard) {
	for _, guard := range guards {
		guard.Release()
	}
}

func (c *Catalog) acquireRecoveryGuardLocked(record sessionRecord, guard protocol.TrashGuard) (*agent.SessionRemovalGuard, error) {
	switch guard {
	case protocol.TrashNormal:
		return nil, nil
	case protocol.TrashRedundantRecoveryOnly:
		path := record.Path
		if record.TrashPath != "" {
			path = record.TrashPath
		}
		parentGuard, err := agent.TryAcquireRecoveryParentGuard(path, filepath.Dir(record.Path))
		if err != nil {
			return nil, catalogError(protocol.ErrRecoveryGuardFailed, err)
		}
		return parentGuard, nil
	default:
		return nil, catalogError(protocol.ErrRecoveryGuardFailed, errors.New("invalid recovery guard"))
	}
}

type artifactMove struct {
	from string
	to   string
}

func (c *Catalog) moveToTrashLocked(workspace workspaceRecord, record sessionRecord) (string, []artifactMove, error) {
	if _, err := c.validateLiveSessionRecordLocked(workspace, record); err != nil {
		return "", nil, err
	}
	storeDir, err := c.sessionStoreDirectoryLocked(workspace, false)
	if err != nil {
		return "", nil, err
	}
	trashRoot := filepath.Join(storeDir, ".remote-trash")
	if err := ensurePrivateDirectory(trashRoot); err != nil {
		return "", nil, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if !sessionPathInStore(storeDir, trashRoot) {
		return "", nil, catalogError(protocol.ErrSessionPersistFailed, errors.New("trash root escaped the Session store"))
	}
	container := trashContainer(storeDir, record.ID)
	if _, err := os.Lstat(container); err == nil {
		return "", nil, catalogError(protocol.ErrSessionCleanupPending, errors.New("trash destination already exists"))
	} else if !os.IsNotExist(err) {
		return "", nil, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if err := os.MkdirAll(container, 0o700); err != nil {
		return "", nil, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	destination := filepath.Join(container, filepath.Base(record.Path))
	moves, err := moveArtifactSet(record.Path, destination)
	if err != nil {
		_ = os.RemoveAll(container)
		return "", nil, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	return destination, moves, nil
}

func moveArtifactSet(source, destination string) ([]artifactMove, error) {
	pairs := artifactPairs(source, destination)
	moved := make([]artifactMove, 0, len(pairs))
	for index, pair := range pairs {
		info, err := os.Lstat(pair.from)
		if err != nil {
			if os.IsNotExist(err) && index > 0 {
				continue
			}
			rollbackArtifactMoves(moved)
			return nil, err
		}
		if index == 0 && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			rollbackArtifactMoves(moved)
			return nil, errors.New("Session transcript is not a regular file")
		}
		if _, err := os.Lstat(pair.to); err == nil {
			rollbackArtifactMoves(moved)
			return nil, fmt.Errorf("artifact destination already exists: %s", filepath.Base(pair.to))
		} else if !os.IsNotExist(err) {
			rollbackArtifactMoves(moved)
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(pair.to), 0o700); err != nil {
			rollbackArtifactMoves(moved)
			return nil, err
		}
		if err := os.Rename(pair.from, pair.to); err != nil {
			rollbackArtifactMoves(moved)
			return nil, err
		}
		moved = append(moved, pair)
	}
	return moved, nil
}

func artifactPairs(source, destination string) []artifactMove {
	sources := append([]string{source}, store.SessionSidecarFiles(source)...)
	sources = append(sources, store.SessionCheckpointDir(source), store.SessionJobsDir(source), store.SessionCleanupPending(source))
	destinations := append([]string{destination}, store.SessionSidecarFiles(destination)...)
	destinations = append(destinations, store.SessionCheckpointDir(destination), store.SessionJobsDir(destination), store.SessionCleanupPending(destination))
	pairs := make([]artifactMove, 0, len(sources))
	for index := range sources {
		pairs = append(pairs, artifactMove{from: sources[index], to: destinations[index]})
	}
	return pairs
}

func rollbackArtifactMoves(moves []artifactMove) {
	for index := len(moves) - 1; index >= 0; index-- {
		_ = os.MkdirAll(filepath.Dir(moves[index].from), 0o700)
		_ = os.Rename(moves[index].to, moves[index].from)
	}
}

func removeEmptyMoveDestinations(moves []artifactMove) {
	seen := make(map[string]bool)
	for _, move := range moves {
		dir := filepath.Dir(move.to)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		_ = os.Remove(dir)
	}
}

func trashContainer(storeDir string, id protocol.SessionID) string {
	digest := sha256.Sum256([]byte(id))
	return filepath.Join(storeDir, ".remote-trash", fmt.Sprintf("session-%x", digest[:16]))
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("catalog directory is not a real directory")
	}
	return os.Chmod(path, 0o700)
}

func (c *Catalog) validateTrashPathLocked(workspace workspaceRecord, record sessionRecord) error {
	storeDir, err := c.sessionStoreDirectoryLocked(workspace, false)
	if err != nil {
		return err
	}
	expected := trashContainer(storeDir, record.ID)
	if pathKey(filepath.Dir(record.TrashPath)) != pathKey(expected) || !sessionPathInStore(storeDir, expected) || !sessionPathInStore(expected, record.TrashPath) {
		return catalogError(protocol.ErrSessionPersistFailed, errors.New("trash artifact escaped its identity container"))
	}
	containerInfo, err := os.Lstat(expected)
	if err != nil || !containerInfo.IsDir() || containerInfo.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			err = errors.New("trash identity container is not a directory")
		}
		return catalogError(protocol.ErrSessionPersistFailed, err)
	}
	info, err := os.Lstat(record.TrashPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			err = errors.New("trash transcript is not a regular file")
		}
		return catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if err := validateMetaSidecar(record.TrashPath, true); err != nil {
		return err
	}
	return nil
}

func (c *Catalog) restoreDestinationLocked(workspace workspaceRecord, record sessionRecord) (string, error) {
	storeDir, err := c.sessionStoreDirectoryLocked(workspace, true)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(storeDir, filepath.Base(record.Path))
	if !artifactSetExists(candidate) {
		return candidate, nil
	}
	digest := sha256.Sum256([]byte(record.ID))
	ext := filepath.Ext(candidate)
	stem := strings.TrimSuffix(filepath.Base(candidate), ext)
	for attempt := 0; attempt < 100; attempt++ {
		name := fmt.Sprintf("%s-restored-%x-%02d%s", stem, digest[:4], attempt, ext)
		path := filepath.Join(storeDir, name)
		if !artifactSetExists(path) {
			return path, nil
		}
	}
	return "", catalogError(protocol.ErrSessionPersistFailed, errors.New("could not allocate restore destination"))
}

func artifactSetExists(path string) bool {
	for _, pair := range artifactPairs(path, path) {
		if _, err := os.Lstat(pair.from); err == nil || !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func (c *Catalog) topicHasSessionsLocked(workspaceID protocol.WorkspaceID, topicID protocol.TopicID) bool {
	for _, session := range c.state.Sessions {
		if session.WorkspaceID == workspaceID && session.TopicID == topicID {
			return true
		}
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
