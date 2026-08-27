package sessioncatalog

import "context"

type sessionUpsertMode uint8

const (
	upsertExactSource sessionUpsertMode = iota
	upsertDirectoryProjection
)

// sessionProjectionFieldsBelongToDirectory reports whether an exact-path
// record must retain the last complete directory projection. A physical
// recovery file cannot prove its logical topic, canonical leaf, or ordinary
// visibility without seeing its siblings and ancestors.
func sessionProjectionFieldsBelongToDirectory(existing, incoming SessionRecord) bool {
	return existing.Recovered || incoming.Recovered ||
		existing.RecoveryGroupID != "" || incoming.RecoveryGroupID != "" ||
		existing.RecoveryRole != "" && existing.RecoveryRole != RecoveryRoleNormal ||
		incoming.RecoveryRole != "" && incoming.RecoveryRole != RecoveryRoleNormal
}

func preserveDirectoryProjection(existing SessionRecord, record *SessionRecord) {
	record.RecoveryCopy = existing.RecoveryCopy
	record.RecoveryGroupID = existing.RecoveryGroupID
	record.RecoveryRole = existing.RecoveryRole
	record.RecoveryCanonical = existing.RecoveryCanonical
	record.LogicalTopicID = existing.LogicalTopicID
	record.OrdinaryVisible = existing.OrdinaryVisible
	if sessionProjectionFieldsBelongToDirectory(existing, *record) {
		record.TopicID = existing.TopicID
		record.TopicTitle = existing.TopicTitle
	}
}

// prepareExactPathProjection avoids publishing unchanged snapshots while
// retaining known counts when only legacy sidecar metadata is stale.
func (c *Catalog) prepareExactPathProjection(ctx context.Context, raw SessionRecord) (record SessionRecord, skip, projectionDirty bool, err error) {
	record = classifyRecoveryLineage(normalizeSessionRecord(raw))
	if record.LogicalTopicID == "" {
		record.LogicalTopicID = record.TopicID
	}
	existing, ok, err := c.GetSession(ctx, record.Path)
	if err != nil {
		return SessionRecord{}, false, false, err
	}
	if !ok || existing.Path == "" || existing.MissingSince != 0 {
		if record.Recovered {
			// A brand-new recovery file has no sibling-aware logical identity yet.
			// Store a hidden physical shell and let the queued directory pass
			// publish its final topic and representative atomically.
			record.TopicID = ""
			record.TopicTitle = ""
			record.LogicalTopicID = ""
			record.OrdinaryVisible = false
		}
		return record, false, record.Recovered, nil
	}
	projectionDirty = existing.TopicID != raw.TopicID ||
		existing.Recovered != record.Recovered ||
		existing.ParentID != record.ParentID ||
		existing.RecoveryDigest != record.RecoveryDigest ||
		existing.RecoveryReason != record.RecoveryReason
	if sessionProjectionFieldsBelongToDirectory(existing, record) && existing.MetaFingerprint != record.MetaFingerprint {
		// The persisted projection intentionally does not duplicate source and
		// projected topic ids. A changed recovery sidecar may therefore have
		// changed a lineage input that only a full directory pass can prove.
		projectionDirty = true
	}
	preserveDirectoryProjection(existing, &record)
	if sameSessionIndexInput(existing, record) {
		return record, true, projectionDirty, nil
	}
	// A sidecar can be observed in an older, transient state while the
	// transcript itself has not changed. Never let an exact-path refresh move a
	// known conversation backwards in the activity-sorted sidebar.
	if existing.ContentFingerprint == record.ContentFingerprint &&
		existing.LastActivityAt > record.LastActivityAt {
		record.LastActivityAt = existing.LastActivityAt
	}
	if existing.TurnsState != TurnsUnknown && record.TurnsState == TurnsUnknown &&
		existing.ContentFingerprint == record.ContentFingerprint {
		record.Preview = existing.Preview
		record.Turns = existing.Turns
		record.TurnsState = existing.TurnsState
	}
	return record, false, projectionDirty, nil
}
