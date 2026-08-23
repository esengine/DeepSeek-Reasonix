package sessioncatalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/agent"
)

// classifyRecoveryLineage assigns recovery_group_id, recovery_role, and
// recovery_canonical from real content ancestry. File names alone never decide
// ownership; covered copies are those whose messages are a prefix of a still-
// present parent/ancestor, adopted is a unique leaf covering the group, and
// diverged marks multiple non-covering leaves under one group.
func classifyRecoveryLineage(record SessionRecord) SessionRecord {
	if !record.Recovered {
		if record.RecoveryRole == "" {
			record.RecoveryRole = RecoveryRoleNormal
		}
		return record
	}
	parentPath := recoveryParentPath(record)
	groupID := firstNonEmpty(record.ParentID, agent.BranchID(record.Path))
	record.RecoveryGroupID = groupID

	if record.RecoveryCopy || (parentPath != "" && agent.RecoveryBranchCoveredByParent(record.Path, filepath.Dir(record.Path))) {
		record.RecoveryCopy = true
		record.RecoveryRole = RecoveryRoleCoveredCopy
		record.RecoveryCanonical = false
		return record
	}
	// Default for non-covered recovery leaves: diverged until a group pass
	// promotes a unique covering leaf to adopted/canonical.
	record.RecoveryRole = RecoveryRoleDiverged
	record.RecoveryCanonical = false
	return record
}

func classifyRecoveryLineageFromContent(record SessionRecord) SessionRecord {
	record.RecoveryCopy = false
	return classifyRecoveryLineage(record)
}

// promoteCanonicalLeaves marks unique non-covered leaves that cover every
// ancestor in their group as adopted/canonical. Multiple non-covering leaves
// stay diverged. open/running/pinned/leased decisions are left to callers.
func promoteCanonicalLeaves(records []SessionRecord) []SessionRecord {
	byGroup, groupRoot := recoveryLineageGroups(records)
	for groupID, idxs := range byGroup {
		for _, i := range idxs {
			records[i].RecoveryRole = RecoveryRoleDiverged
			records[i].RecoveryCanonical = false
		}
		rootIndex, hasRoot := groupRoot[groupID]
		preferred := -1
		for _, index := range idxs {
			if records[index].RecoveryPreferred {
				if preferred >= 0 {
					preferred = -2 // multiple stale preferences fail closed
					break
				}
				preferred = index
			}
		}
		if preferred >= 0 {
			records[preferred].RecoveryRole = RecoveryRolePreferred
			records[preferred].RecoveryCanonical = true
			continue
		}
		contentIdxs := append([]int{}, idxs...)
		if hasRoot {
			contentIdxs = append(contentIdxs, rootIndex)
		}
		content := loadRecoveryGroupContent(records, contentIdxs)
		candidate, ok := uniqueLongestRecovery(records, idxs, content)
		if !ok {
			// No unique covering leaf: still pick one stable representative so
			// ordinary visibility never expands into a wall of forks.
			if len(idxs) > 0 {
				stable := pickPreferredRecovery(indexesToRecords(records, idxs))
				for _, i := range idxs {
					if records[i].Path == stable.Path {
						records[i].RecoveryCanonical = true
						break
					}
				}
			}
			continue
		}
		if hasRoot && !recoveryCandidateCovers(candidate, rootIndex, idxs, content) {
			records[candidate].RecoveryCanonical = true
			continue
		}
		if !hasRoot {
			// Root missing: the unique covering leaf is still the ordinary
			// representative, but without a parent it cannot prove adoption.
			records[candidate].RecoveryCanonical = true
			for _, i := range idxs {
				if i == candidate {
					continue
				}
				if member, ok := content[i]; ok {
					if candidateContent, ok := content[candidate]; ok && candidateContent.Covers(member) {
						records[i].RecoveryCopy = true
						records[i].RecoveryRole = RecoveryRoleCoveredCopy
					}
				}
			}
			continue
		}
		records[candidate].RecoveryRole = RecoveryRoleAdopted
		records[candidate].RecoveryCanonical = true
		// Once one leaf contains the entire group, every other recovery
		// member is an ancestor/equivalent copy covered by that canonical.
		// Marking them covered is what lets History and explicit cleanup
		// converge an old chain instead of leaving hundreds of non-canonical
		// "diverged" rows that preserve no unique content.
		for _, i := range idxs {
			if i == candidate {
				continue
			}
			records[i].RecoveryCopy = true
			records[i].RecoveryRole = RecoveryRoleCoveredCopy
		}
	}
	return projectLogicalSessions(records)
}

func indexesToRecords(records []SessionRecord, idxs []int) []SessionRecord {
	out := make([]SessionRecord, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, records[i])
	}
	return out
}

// projectLogicalSessions re-anchors recovered physical files onto one logical
// topic and marks the single ordinary-list representative. Catalog projection
// only — authoritative JSONL/meta topic_id values are never rewritten.
func projectLogicalSessions(records []SessionRecord) []SessionRecord {
	byID := make(map[string]int, len(records))
	for i := range records {
		byID[agent.BranchID(records[i].Path)] = i
		if !records[i].Recovered {
			if records[i].LogicalTopicID == "" {
				records[i].LogicalTopicID = records[i].TopicID
			}
			continue
		}
		// Filename fallback when meta ParentID is empty.
		if strings.TrimSpace(records[i].ParentID) == "" {
			if parent, ok := agent.RecoveryFilenameParentID(records[i].Path); ok {
				records[i].ParentID = parent
				if records[i].RecoveryGroupID == "" {
					records[i].RecoveryGroupID = parent
				}
			}
		}
		if records[i].RecoveryGroupID == "" {
			records[i].RecoveryGroupID = firstNonEmpty(records[i].ParentID, agent.BranchID(records[i].Path))
		}
	}
	// Walk parent chains so intermediate recovery files also share the root id.
	for i := range records {
		if !records[i].Recovered {
			continue
		}
		groupID := strings.TrimSpace(records[i].RecoveryGroupID)
		seen := map[string]struct{}{}
		parentID := strings.TrimSpace(records[i].ParentID)
		for parentID != "" {
			if _, loop := seen[parentID]; loop {
				break
			}
			seen[parentID] = struct{}{}
			parentIndex, ok := byID[parentID]
			if !ok {
				groupID = firstNonEmpty(groupID, parentID)
				break
			}
			parent := records[parentIndex]
			if !parent.Recovered {
				groupID = parentID
				break
			}
			parentID = strings.TrimSpace(parent.ParentID)
			if parent.RecoveryGroupID != "" {
				groupID = parent.RecoveryGroupID
			}
		}
		if groupID != "" {
			records[i].RecoveryGroupID = groupID
		}
	}

	type groupAnchor struct {
		topicID    string
		topicTitle string
		createdAt  int64
		rootIndex  int
	}
	anchors := map[string]*groupAnchor{}
	for i, rec := range records {
		if !rec.Recovered {
			id := agent.BranchID(rec.Path)
			anchors[id] = &groupAnchor{
				topicID: rec.TopicID, topicTitle: rec.TopicTitle,
				createdAt: rec.CreatedAt, rootIndex: i,
			}
		}
	}
	// Fill missing logical topics from recovered members (root topic empty or
	// root missing). Prefer earliest non-empty member topic_id.
	for _, rec := range records {
		if !rec.Recovered {
			continue
		}
		groupID := strings.TrimSpace(rec.RecoveryGroupID)
		if groupID == "" {
			continue
		}
		anchor := anchors[groupID]
		if anchor == nil {
			anchor = &groupAnchor{rootIndex: -1, createdAt: rec.CreatedAt}
			anchors[groupID] = anchor
		}
		topic := strings.TrimSpace(rec.TopicID)
		if topic == "" {
			continue
		}
		if strings.TrimSpace(anchor.topicID) == "" ||
			(rec.CreatedAt > 0 && (anchor.createdAt == 0 || rec.CreatedAt < anchor.createdAt) &&
				(anchor.rootIndex < 0 || strings.TrimSpace(records[anchor.rootIndex].TopicID) == "")) {
			// Keep a non-empty root topic when present; only override empty roots.
			if anchor.rootIndex >= 0 && strings.TrimSpace(records[anchor.rootIndex].TopicID) != "" {
				continue
			}
			anchor.topicID = topic
			anchor.topicTitle = rec.TopicTitle
			if rec.CreatedAt > 0 {
				anchor.createdAt = rec.CreatedAt
			}
		}
	}
	for groupID, anchor := range anchors {
		if strings.TrimSpace(anchor.topicID) == "" {
			// Stable internal topic so rootless recovery storms still collapse.
			anchor.topicID = "recovery:" + groupID
			if anchor.topicTitle == "" {
				anchor.topicTitle = groupID
			}
		}
	}
	for i := range records {
		groupID := ""
		if records[i].Recovered {
			groupID = strings.TrimSpace(records[i].RecoveryGroupID)
		} else {
			groupID = agent.BranchID(records[i].Path)
		}
		anchor := anchors[groupID]
		if anchor == nil {
			records[i].LogicalTopicID = firstNonEmpty(records[i].TopicID, "recovery:"+agent.BranchID(records[i].Path))
			continue
		}
		records[i].LogicalTopicID = anchor.topicID
		// Catalog ListTopics keys off TopicID. Re-anchor recovered physical
		// rows (and roots that only inherited a member topic) onto one logical
		// topic so pagination never materializes recovery replica walls.
		if anchor.topicID != "" && (records[i].Recovered || strings.TrimSpace(records[i].TopicID) == "") {
			records[i].TopicID = anchor.topicID
		}
		if anchor.topicTitle != "" && (records[i].Recovered || strings.TrimSpace(records[i].TopicTitle) == "") {
			records[i].TopicTitle = anchor.topicTitle
		}
	}

	preferred := PreferredOrdinarySessionPaths(records)
	for i := range records {
		_, records[i].OrdinaryVisible = preferred[strings.TrimSpace(records[i].Path)]
		// When a group has a normal root, the root is the ordinary row even if
		// an adopted leaf is the open target (canonical path).
		if !records[i].Recovered {
			records[i].OrdinaryVisible = true
		}
	}
	// Exactly one ordinary-visible recovered leaf when the root is missing.
	byGroup := map[string][]int{}
	rootPresent := map[string]bool{}
	for i, rec := range records {
		if !rec.Recovered {
			rootPresent[agent.BranchID(rec.Path)] = true
			continue
		}
		if rec.RecoveryCopy || rec.RecoveryRole == RecoveryRoleCoveredCopy {
			records[i].OrdinaryVisible = false
			continue
		}
		byGroup[rec.RecoveryGroupID] = append(byGroup[rec.RecoveryGroupID], i)
	}
	for groupID, idxs := range byGroup {
		if rootPresent[groupID] {
			for _, i := range idxs {
				records[i].OrdinaryVisible = false
			}
			continue
		}
		best := -1
		for _, i := range idxs {
			if records[i].RecoveryCanonical {
				best = i
				break
			}
		}
		if best < 0 {
			bestIdx := pickPreferredRecovery(indexesToRecords(records, idxs))
			for _, i := range idxs {
				if records[i].Path == bestIdx.Path {
					best = i
					break
				}
			}
		}
		for _, i := range idxs {
			records[i].OrdinaryVisible = i == best
		}
	}
	return records
}

func recoveryLineageGroups(records []SessionRecord) (map[string][]int, map[string]int) {
	byID := make(map[string]int, len(records))
	for i := range records {
		byID[agent.BranchID(records[i].Path)] = i
	}
	byGroup := map[string][]int{}
	groupRoot := map[string]int{}
	for i := range records {
		rec := records[i]
		if !rec.Recovered {
			continue
		}
		parentID := strings.TrimSpace(rec.ParentID)
		seen := map[string]struct{}{}
		for parentID != "" {
			if _, loop := seen[parentID]; loop {
				parentID = ""
				break
			}
			seen[parentID] = struct{}{}
			parentIndex, ok := byID[parentID]
			if !ok {
				parentID = ""
				break
			}
			parent := records[parentIndex]
			if !parent.Recovered {
				groupRoot[parentID] = parentIndex
				break
			}
			parentID = strings.TrimSpace(parent.ParentID)
		}
		if parentID != "" {
			records[i].RecoveryGroupID = parentID
		}
	}
	for i, rec := range records {
		if rec.RecoveryGroupID == "" || rec.RecoveryRole == RecoveryRoleCoveredCopy || rec.RecoveryRole == RecoveryRoleNormal {
			continue
		}
		byGroup[rec.RecoveryGroupID] = append(byGroup[rec.RecoveryGroupID], i)
	}
	return byGroup, groupRoot
}

func loadRecoveryGroupContent(records []SessionRecord, idxs []int) map[int]agent.SessionContentSnapshot {
	content := make(map[int]agent.SessionContentSnapshot, len(idxs))
	for _, index := range idxs {
		if _, loaded := content[index]; loaded {
			continue
		}
		if snapshot, ok := agent.LoadSessionContentSnapshot(records[index].Path); ok {
			content[index] = snapshot
		}
	}
	return content
}

func uniqueLongestRecovery(records []SessionRecord, idxs []int, content map[int]agent.SessionContentSnapshot) (int, bool) {
	if len(idxs) == 0 {
		return 0, false
	}
	// Turns/preview metadata is repaired asynchronously and must not decide
	// lineage. Doing so made the first scan permanently label every legacy fork
	// diverged when its sidecar still had turns_state=unknown. Instead, find the
	// leaves that actually cover every recovered member. Equivalent leaves are
	// safe to collapse and use a deterministic activity/path tie-breaker.
	maxLen := -1
	for _, index := range idxs {
		if snapshot, ok := content[index]; ok && snapshot.Len() > maxLen {
			maxLen = snapshot.Len()
		}
	}
	if maxLen < 0 {
		return 0, false
	}
	candidates := make([]int, 0, 1)
	for _, candidate := range idxs {
		candidateContent, ok := content[candidate]
		if !ok || candidateContent.Len() != maxLen {
			continue
		}
		coversAll := true
		for _, other := range idxs {
			otherContent, ok := content[other]
			if !ok || (candidate != other && !candidateContent.Covers(otherContent)) {
				coversAll = false
				break
			}
		}
		if coversAll {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return 0, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := records[candidates[i]], records[candidates[j]]
		if a.LastActivityAt != b.LastActivityAt {
			return a.LastActivityAt > b.LastActivityAt
		}
		return a.Path < b.Path
	})
	return candidates[0], true
}

func recoveryCandidateCovers(candidate, root int, idxs []int, content map[int]agent.SessionContentSnapshot) bool {
	candidateContent, ok := content[candidate]
	if !ok {
		return false
	}
	rootContent, ok := content[root]
	if !ok || !candidateContent.Covers(rootContent) {
		return false
	}
	for _, i := range idxs {
		memberContent, ok := content[i]
		if !ok || (i != candidate && !candidateContent.Covers(memberContent)) {
			return false
		}
	}
	return true
}

func (c *Catalog) refreshDirectoryRecoveryLineage(ctx context.Context, target DirectoryTarget, records []SessionRecord) error {
	for i := range records {
		records[i] = classifyRecoveryLineageFromContent(normalizeSessionRecord(records[i]))
	}
	records = promoteCanonicalLeaves(records)
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	affected := map[TopicKey]struct{}{}
	changed := false
	for _, record := range records {
		// Capture the previously projected topic so re-anchored recovery rows
		// can delete empty legacy topic shells.
		var previous TopicKey
		if err := tx.QueryRowContext(ctx, `SELECT scope,workspace_root,topic_id FROM catalog_sessions WHERE path=?`, record.Path).
			Scan(&previous.Scope, &previous.WorkspaceRoot, &previous.TopicID); err == nil && previous.TopicID != "" {
			affected[previous] = struct{}{}
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE catalog_sessions SET recovered=?,recovery_copy=?,recovery_group_id=?,recovery_role=?,recovery_canonical=?,topic_id=?,topic_title=?,logical_topic_id=?,ordinary_visible=?,parent_id=? WHERE path=?`,
			boolToInt(record.Recovered), boolToInt(record.RecoveryCopy), record.RecoveryGroupID, record.RecoveryRole,
			boolToInt(record.RecoveryCanonical), record.TopicID, record.TopicTitle, record.LogicalTopicID,
			boolToInt(record.OrdinaryVisible), record.ParentID, record.Path)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if rows, _ := result.RowsAffected(); rows > 0 {
			changed = true
		}
		if record.TopicID != "" {
			affected[TopicKey{Scope: record.Scope, WorkspaceRoot: record.WorkspaceRoot, TopicID: record.TopicID}] = struct{}{}
		}
		if record.Recovered && previous.TopicID != "" && previous.TopicID != record.TopicID {
			// The lineage re-anchor moved this recovery row off its old topic;
			// tombstone it so SyncMetadata cannot resurrect the folded shell.
			if err := rememberFoldedTopic(ctx, tx, previous, c.opts.Now().UnixMilli()); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if !changed {
		return tx.Rollback()
	}
	for key := range affected {
		if err := recomputeTopic(ctx, tx, key); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	revision, err := bumpRevision(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Recovery lineage is the final projection step inside ReconcileDirectory.
	// Keep its revision internal; finishDirectoryScan publishes the one complete
	// directory snapshot after missing-row cleanup and readiness are committed.
	c.rememberRevision(revision)
	return nil
}

func recoveryParentPath(record SessionRecord) string {
	parentID := strings.TrimSpace(record.ParentID)
	if parentID == "" {
		return ""
	}
	dir := filepath.Dir(record.Path)
	if dir == "" || dir == "." {
		return ""
	}
	candidate := filepath.Join(dir, parentID+".jsonl")
	return candidate
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// CanonicalSessionPathForTopic returns the path that open/restore should bind
// when a unique adopted/canonical leaf exists for the topic's sessions.
// Empty means keep the caller's path.
func CanonicalSessionPathForTopic(sessions []SessionRecord, current string) string {
	var canonical string
	canonicalRole := ""
	for _, s := range sessions {
		if s.RecoveryCanonical && (s.RecoveryRole == RecoveryRoleAdopted || s.RecoveryRole == RecoveryRolePreferred) {
			if canonical != "" && canonical != s.Path {
				// Ambiguous: do not retarget.
				return ""
			}
			canonical = s.Path
			canonicalRole = s.RecoveryRole
		}
	}
	if canonicalRole == RecoveryRolePreferred && current != "" {
		for _, session := range sessions {
			if session.Path == current && session.Recovered {
				// An explicit click on another recovery leaf is inspection, not a
				// request to follow the default choice.
				return ""
			}
		}
	}
	if canonical == "" || canonical == current {
		return ""
	}
	return canonical
}

// OrdinaryContinuePath returns a safe continuation when current is the ordinary
// parent. Explicit recovery paths stay put so History can inspect them.
func OrdinaryContinuePath(sessions []SessionRecord, current string) string {
	canonical := CanonicalSessionPathForTopic(sessions, current)
	if canonical == "" {
		canonical = uniqueLinearRecoveryLeaf(sessions)
	}
	current = strings.TrimSpace(current)
	if canonical == "" || current == canonical {
		return ""
	}
	if current == "" {
		return canonical
	}
	for _, session := range sessions {
		if session.Path != current {
			continue
		}
		if session.Recovered {
			return ""
		}
		return canonical
	}
	return canonical
}

// uniqueLinearRecoveryLeaf selects the only monotonic descendant in a complete
// parent chain. It is an open target only; content coverage still exclusively
// controls adoption and cleanup.
func uniqueLinearRecoveryLeaf(sessions []SessionRecord) string {
	if len(sessions) < 2 {
		return ""
	}
	byID := make(map[string]int, len(sessions))
	root := -1
	recovered := 0
	for i, session := range sessions {
		id := agent.BranchID(session.Path)
		if id == "" {
			return ""
		}
		if _, duplicate := byID[id]; duplicate {
			return ""
		}
		byID[id] = i
		if session.Recovered {
			recovered++
			continue
		}
		if root >= 0 {
			return ""
		}
		root = i
	}
	if root < 0 || recovered == 0 {
		return ""
	}
	children := make(map[string]int, recovered)
	for i, session := range sessions {
		if !session.Recovered {
			continue
		}
		parentID := strings.TrimSpace(session.ParentID)
		parent, ok := byID[parentID]
		if parentID == "" || !ok {
			return ""
		}
		if _, forked := children[parentID]; forked {
			return ""
		}
		if session.TurnsState == TurnsValid && sessions[parent].TurnsState == TurnsValid && session.Turns < sessions[parent].Turns {
			return ""
		}
		if session.LastActivityAt > 0 && sessions[parent].LastActivityAt > 0 && session.LastActivityAt < sessions[parent].LastActivityAt {
			return ""
		}
		children[parentID] = i
	}
	seen := make(map[int]struct{}, recovered+1)
	leaf := root
	for {
		if _, duplicate := seen[leaf]; duplicate {
			return ""
		}
		seen[leaf] = struct{}{}
		next, ok := children[agent.BranchID(sessions[leaf].Path)]
		if !ok {
			break
		}
		leaf = next
	}
	if len(seen) != len(sessions) || !sessions[leaf].Recovered {
		return ""
	}
	return strings.TrimSpace(sessions[leaf].Path)
}
