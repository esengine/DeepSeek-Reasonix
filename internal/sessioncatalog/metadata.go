package sessioncatalog

import (
	"context"
	"strings"
)

// SyncMetadata projects the small desktop project/topic registries. It never
// removes session-derived topics: an older CLI or a concurrently running
// Reasonix process may have written authoritative sidecars not yet reflected in
// desktop-projects.json.
func (c *Catalog) SyncMetadata(ctx context.Context, projects []ProjectRecord, topics []TopicMetadata) error {
	if c == nil || c.db == nil {
		return nil
	}
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	roots := map[string]struct{}{}
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_projects`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_topics SET metadata_present=0`); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, project := range projects {
		project.Scope, project.WorkspaceRoot = normalizeScope(project.Scope, project.WorkspaceRoot)
		if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_projects(
            scope,workspace_root,title,color,pinned,sort_order,updated_at
        ) VALUES(?,?,?,?,?,?,?) ON CONFLICT(scope,workspace_root) DO UPDATE SET
            title=excluded.title,color=excluded.color,pinned=excluded.pinned,
            sort_order=excluded.sort_order,updated_at=excluded.updated_at`,
			project.Scope, project.WorkspaceRoot, project.Title, project.Color,
			project.Pinned, project.SortOrder, c.opts.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		roots[project.WorkspaceRoot] = struct{}{}
	}
	for _, topic := range topics {
		topic.Scope, topic.WorkspaceRoot = normalizeScope(topic.Scope, topic.WorkspaceRoot)
		if strings.TrimSpace(topic.TopicID) == "" {
			continue
		}
		// Inherit live session aggregates when present so a metadata-only insert
		// does not publish last_activity_at=0 / turns_state=valid and reorder the
		// sidebar ahead of (or instead of) the authoritative session rows.
		if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_topics(
            scope,workspace_root,topic_id,title,title_source,pinned,sort_order,
            turns,turns_state,created_at,last_activity_at,recovery_state,health,metadata_present
        )
        SELECT ?,?,?,?,?,?,?,
			COALESCE((SELECT MAX(
				COALESCE(SUM(CASE WHEN recovery_copy=0 AND recovered=0 AND turns_state='valid' THEN turns ELSE 0 END),0),
				COALESCE(MAX(CASE WHEN recovery_copy=0 AND recovered=1 AND turns_state='valid' THEN turns ELSE 0 END),0)
			) FROM catalog_sessions WHERE scope=? AND workspace_root=? AND topic_id=?),0),
            COALESCE((SELECT CASE
                WHEN SUM(CASE WHEN recovery_copy=0 AND turns_state='corrupt' THEN 1 ELSE 0 END)>0 THEN 'corrupt'
                WHEN SUM(CASE WHEN recovery_copy=0 AND turns_state='unknown' THEN 1 ELSE 0 END)>0 THEN 'unknown'
                WHEN SUM(CASE WHEN recovery_copy=0 THEN 1 ELSE 0 END)=0 AND COUNT(*)>0 THEN 'valid'
                WHEN COUNT(*)=0 THEN 'unknown'
                ELSE 'valid' END
                FROM catalog_sessions WHERE scope=? AND workspace_root=? AND topic_id=?),'unknown'),
            COALESCE(NULLIF(?,0),(SELECT MIN(NULLIF(created_at,0)) FROM catalog_sessions
                WHERE scope=? AND workspace_root=? AND topic_id=?),0),
            COALESCE((SELECT MAX(last_activity_at) FROM catalog_sessions
                WHERE scope=? AND workspace_root=? AND topic_id=?),0),
            COALESCE((SELECT CASE
                WHEN COUNT(*)>0 AND SUM(CASE WHEN recovery_copy=0 THEN 1 ELSE 0 END)=0 THEN 'recovery_only'
                ELSE '' END
                FROM catalog_sessions WHERE scope=? AND workspace_root=? AND topic_id=?),''),
            COALESCE((SELECT CASE
                WHEN SUM(CASE WHEN recovery_copy=0 AND health='corrupt' THEN 1 ELSE 0 END)>0 THEN 'corrupt'
                WHEN SUM(CASE WHEN recovery_copy=0 AND health='missing' THEN 1 ELSE 0 END)>0 THEN 'missing'
                ELSE 'ok' END
                FROM catalog_sessions WHERE scope=? AND workspace_root=? AND topic_id=?),'ok'),
            1
        ON CONFLICT(scope,workspace_root,topic_id) DO UPDATE SET
            title=CASE WHEN excluded.title<>'' THEN excluded.title ELSE catalog_topics.title END,
            title_source=excluded.title_source,pinned=excluded.pinned,
            sort_order=excluded.sort_order,metadata_present=1,
            created_at=CASE WHEN excluded.created_at>0 THEN excluded.created_at ELSE catalog_topics.created_at END,
            last_activity_at=CASE WHEN excluded.last_activity_at>catalog_topics.last_activity_at
                THEN excluded.last_activity_at ELSE catalog_topics.last_activity_at END,
            turns=CASE WHEN excluded.turns>0 THEN excluded.turns ELSE catalog_topics.turns END,
            turns_state=CASE WHEN excluded.turns_state<>'' AND excluded.turns_state<>'unknown'
                THEN excluded.turns_state ELSE catalog_topics.turns_state END,
            recovery_state=excluded.recovery_state`,
			topic.Scope, topic.WorkspaceRoot, topic.TopicID, topic.Title,
			topic.TitleSource, topic.Pinned, topic.SortOrder,
			topic.Scope, topic.WorkspaceRoot, topic.TopicID,
			topic.Scope, topic.WorkspaceRoot, topic.TopicID,
			topic.CreatedAt, topic.Scope, topic.WorkspaceRoot, topic.TopicID,
			topic.Scope, topic.WorkspaceRoot, topic.TopicID,
			topic.Scope, topic.WorkspaceRoot, topic.TopicID,
			topic.Scope, topic.WorkspaceRoot, topic.TopicID); err != nil {
			_ = tx.Rollback()
			return err
		}
		roots[topic.WorkspaceRoot] = struct{}{}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_topics
        WHERE metadata_present=0 AND NOT EXISTS (
            SELECT 1 FROM catalog_sessions s WHERE s.scope=catalog_topics.scope
            AND s.workspace_root=catalog_topics.workspace_root AND s.topic_id=catalog_topics.topic_id
        )`); err != nil {
		_ = tx.Rollback()
		return err
	}
	revision, err := bumpRevision(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.publishRevision(revision, mapKeys(roots), "metadata")
	return nil
}
