package sessioncatalog

import (
	"context"
	"database/sql"
	"path/filepath"
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
		skip, err := skipFoldedRecoveryShell(ctx, tx, topic)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if skip {
			continue
		}
		if err := upsertTopicMetadata(ctx, tx, topic); err != nil {
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

// skipFoldedRecoveryShell reports whether SyncMetadata must not (re)create a
// metadata topic shell for a folded recovery copy. While a directory scan is
// pending, the copy's rows may still sit under their pre-reanchor topic; once
// lineage projection re-anchors them onto the canonical row, re-creating this
// shell from the registry would re-list the copy as a separate sidebar session
// (#8525/#8551). Explicitly pinned topics survive: the user asked for that row.
func skipFoldedRecoveryShell(ctx context.Context, tx *sql.Tx, topic TopicMetadata) (bool, error) {
	if topic.Pinned {
		return false, nil
	}
	return foldedRecoveryShellHasCanonical(ctx, tx, topic.Scope, topic.WorkspaceRoot, topic.TopicID)
}

// upsertTopicMetadata applies one registry topic. It inherits live session
// aggregates when present so a metadata-only insert does not publish
// last_activity_at=0 / turns_state=valid and reorder the sidebar ahead of (or
// instead of) the authoritative session rows.
func upsertTopicMetadata(ctx context.Context, tx *sql.Tx, topic TopicMetadata) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO catalog_topics(
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
            title=COALESCE(NULLIF((SELECT s.custom_title FROM catalog_sessions s
                    WHERE s.scope=excluded.scope AND s.workspace_root=excluded.workspace_root
                    AND s.topic_id=excluded.topic_id
                    ORDER BY s.recovery_copy ASC,s.last_activity_at DESC,s.path ASC LIMIT 1),''),
                NULLIF(excluded.title,''),catalog_topics.title),
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
		topic.Scope, topic.WorkspaceRoot, topic.TopicID)
	return err
}

// foldedRecoveryShellHasCanonical reports whether topicID currently projects
// only recovery sessions whose lineage already has an ordinary/canonical
// representative in the catalog, or was tombstoned by a lineage re-anchor.
// Such a topic is a folded recovery copy's shell: its conversation is already
// listed under the canonical row, so SyncMetadata must not (re)create a
// standalone topic for it.
//
// A canonical representative is either a group member flagged
// ordinary_visible/recovery_canonical, or the non-recovered group root (which
// carries no recovery_group_id of its own, so it is matched by path).
// Lineages with no canonical yet (unresolved, still scanning) are left alone.
func foldedRecoveryShellHasCanonical(ctx context.Context, tx *sql.Tx, scope, workspaceRoot, topicID string) (bool, error) {
	var ordinary int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions
		WHERE scope=? AND workspace_root=? AND topic_id=? AND recovered=0 AND recovery_copy=0`,
		scope, workspaceRoot, topicID).Scan(&ordinary); err != nil {
		return false, err
	}
	if ordinary > 0 {
		return false, nil
	}
	var folded int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_folded_topics
		WHERE scope=? AND workspace_root=? AND topic_id=?`,
		scope, workspaceRoot, topicID).Scan(&folded); err != nil {
		return false, err
	}
	if folded > 0 {
		return true, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT directory, recovery_group_id FROM catalog_sessions
		WHERE scope=? AND workspace_root=? AND topic_id=? AND recovered=1 AND recovery_group_id<>''`,
		scope, workspaceRoot, topicID)
	if err != nil {
		return false, err
	}
	type groupRef struct {
		directory string
		id        string
	}
	groups := []groupRef{}
	for rows.Next() {
		var group groupRef
		if err := rows.Scan(&group.directory, &group.id); err != nil {
			rows.Close()
			return false, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	if len(groups) == 0 {
		return false, nil
	}
	for _, group := range groups {
		var canonical int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions
			WHERE scope=? AND workspace_root=? AND recovery_group_id=? AND (ordinary_visible=1 OR recovery_canonical=1)`,
			scope, workspaceRoot, group.id).Scan(&canonical); err != nil {
			return false, err
		}
		if canonical > 0 {
			return true, nil
		}
		rootPath := filepath.Join(group.directory, group.id+".jsonl")
		var roots int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions
			WHERE path=? AND recovered=0 AND recovery_copy=0`, rootPath).Scan(&roots); err != nil {
			return false, err
		}
		if roots > 0 {
			return true, nil
		}
	}
	return false, nil
}

// rememberFoldedTopic tombstones a topic that lineage projection folded into a
// recovery lineage's canonical row. The tombstone is cleared automatically if
// a session is ever indexed under that topic id again.
func rememberFoldedTopic(ctx context.Context, tx *sql.Tx, key TopicKey, foldedAt int64) error {
	if strings.TrimSpace(key.TopicID) == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO catalog_folded_topics(scope,workspace_root,topic_id,folded_at)
		VALUES(?,?,?,?)`, key.Scope, key.WorkspaceRoot, key.TopicID, foldedAt)
	return err
}

// updateFoldedTopicTombstones maintains folded-topic tombstones around a
// session upsert: a session claiming a folded topic id makes it real again,
// and a recovered row moving topics tombstones the shell it left behind.
func updateFoldedTopicTombstones(ctx context.Context, tx *sql.Tx, previous TopicKey, record SessionRecord, now int64) error {
	if record.TopicID != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_folded_topics WHERE scope=? AND workspace_root=? AND topic_id=?`,
			record.Scope, record.WorkspaceRoot, record.TopicID); err != nil {
			return err
		}
	}
	if record.Recovered && previous.TopicID != "" && previous.TopicID != record.TopicID {
		return rememberFoldedTopic(ctx, tx, previous, now)
	}
	return nil
}
