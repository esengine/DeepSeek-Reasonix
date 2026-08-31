package sessioncatalog

import (
	"context"
	"database/sql"
	"errors"
)

func (c *Catalog) UpsertSession(ctx context.Context, record SessionRecord) error {
	record = normalizeSessionRecord(record)
	record.enqueueSequence = c.mutationSeq.Add(1)
	return c.upsertSessions(ctx, []SessionRecord{record}, nil, "write")
}

func (c *Catalog) upsertSessions(ctx context.Context, records []SessionRecord, generations map[string]int64, reason string) error {
	_, err := c.upsertSessionsWithNotification(ctx, records, generations, reason, true, upsertExactSource)
	return err
}

func (c *Catalog) upsertExactPathSession(ctx context.Context, record SessionRecord) (bool, error) {
	dirty, err := c.upsertSessionsWithNotification(ctx, []SessionRecord{record}, nil, "write", true, upsertExactSource)
	return len(dirty) > 0, err
}

func (c *Catalog) upsertSessionsWithNotification(ctx context.Context, records []SessionRecord, generations map[string]int64, reason string, notify bool, mode sessionUpsertMode) (map[string]DirectoryTarget, error) {
	dirtyDirectories := map[string]DirectoryTarget{}
	if len(records) == 0 {
		return dirtyDirectories, nil
	}
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	filtered := records[:0]
	for _, record := range records {
		pathKey := c.pathKey(record.Path)
		if c.pathMutationAllowed(pathKey, record.enqueueSequence) {
			filtered = append(filtered, record)
		}
	}
	records = filtered
	if len(records) == 0 {
		return dirtyDirectories, nil
	}
	if mode == upsertExactSource {
		prepared := make([]SessionRecord, 0, len(records))
		for _, raw := range records {
			record, skip, projectionDirty, err := c.prepareExactPathProjection(ctx, raw)
			if err != nil {
				return dirtyDirectories, err
			}
			if projectionDirty {
				dirtyDirectories[c.pathKey(record.Directory)] = DirectoryTarget{
					Path: record.Directory, Scope: record.Scope, WorkspaceRoot: record.WorkspaceRoot,
				}
			}
			if !skip {
				prepared = append(prepared, record)
			}
		}
		records = prepared
		if len(records) == 0 {
			return dirtyDirectories, nil
		}
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return dirtyDirectories, err
	}
	affected := map[TopicKey]struct{}{}
	roots := map[string]struct{}{}
	directoryGenerations := map[string]int64{}
	for _, raw := range records {
		record := normalizeSessionRecord(raw)
		pathKey := c.pathKey(record.Path)
		directoryKey := c.pathKey(record.Directory)
		remapped, err := removeRemappedSessionIdentity(ctx, tx, record.Path, pathKey)
		if err != nil {
			_ = tx.Rollback()
			return dirtyDirectories, err
		}
		for _, key := range remapped {
			affected[key] = struct{}{}
		}
		var previous TopicKey
		if err := tx.QueryRowContext(ctx, `SELECT scope,workspace_root,workspace_root_key,topic_id FROM catalog_sessions WHERE path_key=?`, pathKey).
			Scan(&previous.Scope, &previous.WorkspaceRoot, &previous.workspaceKey, &previous.TopicID); err == nil && previous.TopicID != "" {
			affected[previous] = struct{}{}
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return dirtyDirectories, err
		}
		generation := int64(0)
		if generations != nil {
			generation = generations[record.Path]
		} else if cached, ok := directoryGenerations[directoryKey]; ok {
			generation = cached
		} else {
			_ = tx.QueryRowContext(ctx, `SELECT scan_generation FROM catalog_directories WHERE path_key=?`, directoryKey).Scan(&generation)
			directoryGenerations[directoryKey] = generation
		}
		if err := c.upsertSessionRow(ctx, tx, record, pathKey, directoryKey, generation, mode); err != nil {
			_ = tx.Rollback()
			return dirtyDirectories, err
		}
		if record.TopicID != "" {
			affected[TopicKey{Scope: record.Scope, WorkspaceRoot: record.WorkspaceRoot,
				workspaceKey: c.workspaceRootKey(record.Scope, record.WorkspaceRoot), TopicID: record.TopicID}] = struct{}{}
		}
		if err := c.updateFoldedTopicTombstones(ctx, tx, previous, record, c.opts.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return dirtyDirectories, err
		}
		roots[record.WorkspaceRoot] = struct{}{}
	}
	for key := range affected {
		if err := c.recomputeTopic(ctx, tx, key); err != nil {
			_ = tx.Rollback()
			return dirtyDirectories, err
		}
	}
	revision, err := bumpRevision(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return dirtyDirectories, err
	}
	if err := tx.Commit(); err != nil {
		return dirtyDirectories, err
	}
	if notify {
		c.publishRevision(revision, mapKeys(roots), reason)
	} else {
		c.rememberRevision(revision)
	}
	c.refreshCounts(ctx)
	return dirtyDirectories, nil
}

const sessionInsertSQL = `INSERT INTO catalog_sessions(
    path,path_key,directory,directory_key,scope,workspace_root,workspace_root_key,topic_id,topic_title,custom_title,
    created_at,last_activity_at,preview,turns,turns_state,recovered,
    recovery_reason,recovery_digest,parent_id,recovery_copy,recovery_group_id,
    recovery_role,recovery_canonical,logical_topic_id,ordinary_visible,content_fingerprint,
    meta_fingerprint,health,missing_since,seen_generation
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(path_key) DO UPDATE SET `

const directoryProjectionUpdateSQL = `
    path=excluded.path, directory=excluded.directory, directory_key=excluded.directory_key, scope=excluded.scope,
    workspace_root=excluded.workspace_root, workspace_root_key=excluded.workspace_root_key, topic_id=excluded.topic_id,
    topic_title=excluded.topic_title, custom_title=excluded.custom_title,
    created_at=excluded.created_at, last_activity_at=excluded.last_activity_at,
    preview=excluded.preview, turns=excluded.turns,
    turns_state=excluded.turns_state, recovered=excluded.recovered,
    recovery_reason=excluded.recovery_reason,
    recovery_digest=excluded.recovery_digest, parent_id=excluded.parent_id,
    recovery_copy=excluded.recovery_copy,
    recovery_group_id=excluded.recovery_group_id,
    recovery_role=excluded.recovery_role,
    recovery_canonical=excluded.recovery_canonical,
    logical_topic_id=excluded.logical_topic_id,
    ordinary_visible=excluded.ordinary_visible,
    content_fingerprint=excluded.content_fingerprint,
    meta_fingerprint=excluded.meta_fingerprint, health=excluded.health,
    missing_since=0, seen_generation=MAX(catalog_sessions.seen_generation, excluded.seen_generation)`

const exactSourceUpdateSQL = `
    path=excluded.path, directory=excluded.directory, directory_key=excluded.directory_key, scope=excluded.scope,
    workspace_root=excluded.workspace_root, workspace_root_key=excluded.workspace_root_key,
    topic_id=CASE
        WHEN catalog_sessions.recovered=1 OR excluded.recovered=1 OR catalog_sessions.recovery_group_id<>''
        THEN catalog_sessions.topic_id ELSE excluded.topic_id END,
    topic_title=CASE
        WHEN catalog_sessions.recovered=1 OR excluded.recovered=1 OR catalog_sessions.recovery_group_id<>''
        THEN catalog_sessions.topic_title ELSE excluded.topic_title END,
    custom_title=excluded.custom_title,
    created_at=excluded.created_at, last_activity_at=excluded.last_activity_at,
    preview=excluded.preview, turns=excluded.turns,
    turns_state=excluded.turns_state, recovered=excluded.recovered,
    recovery_reason=excluded.recovery_reason,
    recovery_digest=excluded.recovery_digest, parent_id=excluded.parent_id,
    recovery_copy=catalog_sessions.recovery_copy,
    recovery_group_id=catalog_sessions.recovery_group_id,
    recovery_role=catalog_sessions.recovery_role,
    recovery_canonical=catalog_sessions.recovery_canonical,
    logical_topic_id=catalog_sessions.logical_topic_id,
    ordinary_visible=catalog_sessions.ordinary_visible,
    content_fingerprint=excluded.content_fingerprint,
    meta_fingerprint=excluded.meta_fingerprint, health=excluded.health,
    missing_since=0, seen_generation=MAX(catalog_sessions.seen_generation, excluded.seen_generation)`

func (c *Catalog) upsertSessionRow(ctx context.Context, tx *sql.Tx, record SessionRecord, pathKey, directoryKey string, generation int64, mode sessionUpsertMode) error {
	updateSQL := directoryProjectionUpdateSQL
	if mode == upsertExactSource {
		updateSQL = exactSourceUpdateSQL
	}
	_, err := tx.ExecContext(ctx, sessionInsertSQL+updateSQL, c.sessionRowValues(record, pathKey, directoryKey, generation)...)
	return err
}

func (c *Catalog) sessionRowValues(record SessionRecord, pathKey, directoryKey string, generation int64) []any {
	return []any{
		record.Path, pathKey, record.Directory, directoryKey, record.Scope, record.WorkspaceRoot,
		c.workspaceRootKey(record.Scope, record.WorkspaceRoot), record.TopicID, record.TopicTitle, record.CustomTitle, record.CreatedAt,
		record.LastActivityAt, record.Preview, record.Turns, record.TurnsState,
		record.Recovered, record.RecoveryReason, record.RecoveryDigest,
		record.ParentID, boolToInt(record.RecoveryCopy), record.RecoveryGroupID,
		record.RecoveryRole, boolToInt(record.RecoveryCanonical),
		record.LogicalTopicID, boolToInt(record.OrdinaryVisible),
		record.ContentFingerprint, record.MetaFingerprint,
		record.Health, 0, generation,
	}
}
