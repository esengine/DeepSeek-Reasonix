package sessioncatalog

import (
	"context"
	"database/sql"
	"errors"

	"reasonix/internal/agent"
)

const (
	repairReasonPathMismatch  = "path_mismatch"
	repairReasonScopeMismatch = "scope_mismatch"
	repairReasonTopicMismatch = "topic_mismatch"
)

// directoryScanCanSkip is deliberately stricter than a row-count check. The
// directory signature tells us that the source entries did not change, but an
// older catalog may still contain the wrong rows for the same count. Compare
// the authoritative path/sidecar projection before trusting the ready marker.
func (c *Catalog) directoryScanCanSkip(ctx context.Context, target DirectoryTarget, signature string) (bool, error) {
	path := target.Path
	target.Scope, target.WorkspaceRoot = normalizeScope(target.Scope, target.WorkspaceRoot)
	var expectedTotal, present, unprojected, missing int
	var storedScope, storedRoot string
	err := c.db.QueryRowContext(ctx, `SELECT scope,workspace_root,total,
		(SELECT COUNT(*) FROM catalog_sessions WHERE directory=? AND missing_since=0),
		(SELECT COUNT(*) FROM catalog_sessions s
		 WHERE s.directory=? AND s.missing_since=0 AND s.topic_id<>''
		 AND NOT EXISTS (
			 SELECT 1 FROM catalog_topics t
			 WHERE t.scope=s.scope AND t.workspace_root=s.workspace_root AND t.topic_id=s.topic_id
		 )),
		(SELECT COUNT(*) FROM catalog_sessions WHERE directory=? AND missing_since>0)
		FROM catalog_directories
		WHERE path=? AND signature=? AND state='ready'`,
		path, path, path, path, signature).Scan(&storedScope, &storedRoot, &expectedTotal, &present, &unprojected, &missing)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if storedScope != target.Scope || storedRoot != target.WorkspaceRoot {
		c.markRepair(repairReasonScopeMismatch, c.opts.Now().UnixMilli())
		return false, nil
	}
	ordered, err := agent.ListSessionOrder(path)
	if err != nil {
		return false, err
	}
	expectedRecords := make([]SessionRecord, 0, len(ordered))
	for _, info := range ordered {
		expectedRecords = append(expectedRecords, normalizeSessionRecord(recordFromOrder(target, info)))
	}
	for i := range expectedRecords {
		expectedRecords[i] = classifyRecoveryLineageFromContent(expectedRecords[i])
	}
	expectedRecords = promoteCanonicalLeaves(expectedRecords)
	expected := make(map[string]SessionRecord, len(expectedRecords))
	for _, record := range expectedRecords {
		expected[record.Path] = record
	}
	if expectedTotal != len(expected) || present != len(expected) {
		c.markRepair(repairReasonPathMismatch, c.opts.Now().UnixMilli())
		return false, nil
	}
	rows, err := c.db.QueryContext(ctx, `SELECT path,directory,scope,workspace_root,
		topic_id,topic_title,custom_title,created_at,last_activity_at,preview,
		turns,turns_state,recovered,recovery_reason,recovery_digest,parent_id,
		recovery_copy,recovery_group_id,recovery_role,recovery_canonical,
		logical_topic_id,ordinary_visible,content_fingerprint,meta_fingerprint,health
		FROM catalog_sessions WHERE directory=? AND missing_since=0`, path)
	if err != nil {
		return false, err
	}
	seen := make(map[string]struct{}, len(expected))
	for rows.Next() {
		var got SessionRecord
		var recovered, recoveryCopy, recoveryCanonical, ordinaryVisible int
		if err := rows.Scan(&got.Path, &got.Directory, &got.Scope, &got.WorkspaceRoot,
			&got.TopicID, &got.TopicTitle, &got.CustomTitle, &got.CreatedAt,
			&got.LastActivityAt, &got.Preview, &got.Turns, &got.TurnsState,
			&recovered, &got.RecoveryReason, &got.RecoveryDigest, &got.ParentID,
			&recoveryCopy, &got.RecoveryGroupID, &got.RecoveryRole, &recoveryCanonical,
			&got.LogicalTopicID, &ordinaryVisible, &got.ContentFingerprint,
			&got.MetaFingerprint, &got.Health); err != nil {
			_ = rows.Close()
			return false, err
		}
		got.Recovered = recovered != 0
		got.RecoveryCopy = recoveryCopy != 0
		got.RecoveryCanonical = recoveryCanonical != 0
		got.OrdinaryVisible = ordinaryVisible != 0
		want, ok := expected[got.Path]
		if !ok {
			_ = rows.Close()
			c.markRepair(repairReasonPathMismatch, c.opts.Now().UnixMilli())
			return false, nil
		}
		seen[got.Path] = struct{}{}
		if !sameSessionProjection(got, want) {
			_ = rows.Close()
			c.markRepair(repairReasonTopicMismatch, c.opts.Now().UnixMilli())
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if len(seen) != len(expected) {
		c.markRepair(repairReasonPathMismatch, c.opts.Now().UnixMilli())
		return false, nil
	}
	if unprojected != 0 || missing != 0 {
		c.markRepair(repairReasonTopicMismatch, c.opts.Now().UnixMilli())
		return false, nil
	}
	return true, nil
}

func sameSessionProjection(got, want SessionRecord) bool {
	return got.Path == want.Path &&
		got.Directory == want.Directory &&
		got.Scope == want.Scope &&
		got.WorkspaceRoot == want.WorkspaceRoot &&
		got.TopicID == want.TopicID &&
		got.TopicTitle == want.TopicTitle &&
		got.CustomTitle == want.CustomTitle &&
		got.CreatedAt == want.CreatedAt &&
		got.LastActivityAt == want.LastActivityAt &&
		got.Preview == want.Preview &&
		got.Turns == want.Turns &&
		got.TurnsState == want.TurnsState &&
		got.Recovered == want.Recovered &&
		got.RecoveryReason == want.RecoveryReason &&
		got.RecoveryDigest == want.RecoveryDigest &&
		got.ParentID == want.ParentID &&
		got.RecoveryCopy == want.RecoveryCopy &&
		got.RecoveryGroupID == want.RecoveryGroupID &&
		got.RecoveryRole == want.RecoveryRole &&
		got.RecoveryCanonical == want.RecoveryCanonical &&
		got.LogicalTopicID == want.LogicalTopicID &&
		got.OrdinaryVisible == want.OrdinaryVisible &&
		got.ContentFingerprint == want.ContentFingerprint &&
		got.MetaFingerprint == want.MetaFingerprint &&
		got.Health == want.Health
}
