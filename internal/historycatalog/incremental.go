package historycatalog

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/store"
)

func sourceProjectionUnchanged(queryErr error, oldContent, content, oldMeta, meta, oldDigest, digest string) bool {
	return queryErr == nil && oldContent == content && oldMeta == meta && (digest == "" || digest == oldDigest)
}

func (c *Catalog) tryAppendPath(ctx context.Context, root Root, path string, generation int64, appendFrom,
	oldMessageCount int, oldRevision, revision int64, digest, contentFingerprint, metaFingerprint string) (bool, error) {
	if appendFrom != oldMessageCount || revision != oldRevision+1 {
		return false, nil
	}
	index, err := agent.LoadSessionDisplayIndex(store.SessionDisplayIndex(path))
	if err != nil || !index.RevisionKnown || index.Revision != revision || index.ContentDigest != digest || index.MessageCount < appendFrom {
		return false, nil
	}
	tail, checkedIndex, err := agent.LoadSessionDisplayMessageRange(path, appendFrom, index.MessageCount)
	if err != nil || checkedIndex.ContentDigest != digest || checkedIndex.Revision != revision {
		return false, nil
	}
	meta, _, _ := agent.LoadBranchMeta(path)
	lastActivity := max(int64(0), agent.SessionContentModTime(path).UnixMilli())
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE history_sources SET root=?,source=?,scope=?,workspace_root=?,content_revision=?,
		content_digest=?,content_fingerprint=?,meta_fingerprint=?,message_count=?,indexed_message_count=?,custom_title=?,topic_id=?,
		topic_title=?,preview=?,created_at=?,last_activity_at=?,health='ok',missing_since=0,
		seen_generation=CASE WHEN ?>0 THEN ? ELSE seen_generation END,last_error=''
		WHERE path=? AND content_revision=? AND indexed_message_count=?`, root.Path, root.Source, root.Scope, root.WorkspaceRoot,
		revision, digest, contentFingerprint, metaFingerprint, index.MessageCount, index.MessageCount, meta.CustomTitle, meta.TopicID,
		meta.TopicTitle, meta.Preview, meta.CreatedAt.UnixMilli(), lastActivity, generation, generation, path, oldRevision, oldMessageCount)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	updated, _ := result.RowsAffected()
	if updated != 1 {
		_ = tx.Rollback()
		return false, nil
	}
	for _, doc := range documents(tail) {
		result, err := tx.ExecContext(ctx, `INSERT INTO history_documents(source_path,message_index,part_index,role,kind,tool_name,token_count)
			VALUES(?,?,?,?,?,?,?)`, path, appendFrom+doc.message, doc.part, doc.role, doc.kind, doc.tool, doc.count)
		if err != nil {
			_ = tx.Rollback()
			return false, err
		}
		rowID, err := result.LastInsertId()
		if err != nil {
			_ = tx.Rollback()
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO history_fts(rowid,terms) VALUES(?,?)`, rowID, doc.terms); err != nil {
			_ = tx.Rollback()
			return false, err
		}
	}
	newRevision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	c.publish(newRevision, []string{root.Path}, "source-appended")
	return true, nil
}
