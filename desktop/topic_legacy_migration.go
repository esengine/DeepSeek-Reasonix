package main

import (
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

// legacyMigrationMu serializes the lockless load-modify-save of the projects /
// topic-title files: this migration runs from every concurrent buildTabController
// and from ListProjectTree, so without it parallel runs lose each other's appends.
var legacyMigrationMu sync.Mutex

func migrateLegacySessionsIntoGlobalTopics(dir string) []string {
	return migrateLegacySessionsIntoGlobalTopicsWithGates(dir, topicMigrationDone, topicIndexRepairDone, ignoreMigratedSession)
}

// forceMigrateLegacySessionsIntoGlobalTopicsWithPaths bypasses disposable
// completion markers for explicit reconciliation. The directory signature
// normally keeps background passes cheap, but no signature can be an authority
// boundary: an old CLI, restored backup, or coarse filesystem timestamp must
// still have a path that deterministically re-evaluates every session.
func forceMigrateLegacySessionsIntoGlobalTopicsWithPaths(dir string) ([]string, []string) {
	paths := []string{}
	topics := migrateLegacySessionsIntoGlobalTopicsWithGates(dir, topicMigrationNeverDone, topicMigrationNeverDone,
		func(path string) { paths = append(paths, path) })
	return topics, paths
}

func topicMigrationNeverDone(string) bool { return false }
func ignoreMigratedSession(string)        {}

func noteMigratedSession(topics []string, topicID, path string, onMigrated func(string)) []string {
	onMigrated(path)
	return append(topics, topicID)
}

func migrateLegacySessionsIntoGlobalTopicsWithGates(dir string, migrationDone, repairDone func(string) bool, onMigrated func(string)) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	repairedTopicIDs := repairIndexedSessionTopicsWithGate(dir, repairDone)
	// One-shot per dir: once the migration pass has completed, skip the full
	// per-render session scan entirely.
	if migrationDone(dir) {
		return repairedTopicIDs
	}
	scope, workspaceRoot, topicTitleRoot, ok := legacyMigrationTargetForDir(dir)
	if !ok {
		return nil
	}
	legacyMigrationMu.Lock()
	defer legacyMigrationMu.Unlock()
	// Re-check under the lock: another render may have completed the pass while
	// this one waited.
	if migrationDone(dir) {
		return nil
	}
	infos, err := agent.ListSessionOrder(dir)
	if err != nil {
		return nil // transient read error — retry on the next render, leave unmarked
	}

	var migratedTopicIDs []string
	var titles map[string]string
	var topicTitles map[string]string
	var topicSources map[string]string
	// deferred stays false only when every session was either migrated or is
	// permanently non-migratable. A transient skip (unreadable meta, empty
	// session that may gain content, failed write) sets it, keeping the dir
	deferred := false
	for _, info := range infos {
		if sessionOrderInfoIsUnmodifiedRecoveryCopy(info, dir) {
			continue
		}
		if strings.TrimSpace(info.TopicID) != "" {
			continue
		}
		if meta, ok, err := agent.LoadBranchMeta(info.Path); err != nil {
			deferred = true
			continue
		} else if ok && !legacySessionMetaMatchesMigrationTarget(meta, scope, workspaceRoot) {
			continue
		}
		topicID := legacySessionTopicID(info.Path)
		if topicID == "" {
			continue
		}
		preview, turns := agent.SessionPreview(info.Path)
		if turns == 0 {
			deferred = true // empty now, but a later turn could make it migratable
			continue
		}
		if titles == nil {
			titles = loadSessionTitles(dir)
		}
		title := strings.TrimSpace(titles[filepath.Base(info.Path)])
		if title == "" {
			title = topicTitleFromText(preview)
		} else if normalized := topicTitleFromText(title); normalized != "" {
			title = normalized
		}
		if title == "" {
			when := info.LastActivityAt
			if when.IsZero() {
				when = info.ModTime
			}
			if when.IsZero() {
				title = "历史会话"
			} else {
				title = "历史会话 " + when.Local().Format("2006-01-02")
			}
		}

		migrated, err := func() (bool, error) {
			// Read-modify-write on the branch-meta sidecar: hold the per-path
			// meta lock so agent-side writers (autosave revision bumps,
			// in-flight markers) can't interleave between the load and save
			unlock, lockErr := agent.LockSessionMetaPath(info.Path)
			if lockErr != nil {
				return false, lockErr
			}
			defer unlock()
			meta, err := agent.EnsureBranchMetaLocked(info.Path)
			if err != nil {
				return false, err
			}
			// Preserve scoped sessions only when their existing ownership matches
			// the directory being migrated.
			if !legacySessionMetaMatchesMigrationTarget(meta, scope, workspaceRoot) {
				return false, nil
			}
			meta.Scope = scope
			meta.WorkspaceRoot = workspaceRoot
			meta.TopicID = topicID
			meta.TopicTitle = title
			return true, agent.SaveBranchMetaPreserveUpdatedLocked(info.Path, meta)
		}()
		if err != nil {
			deferred = true
			continue
		}
		if !migrated {
			continue
		}
		if topicTitles == nil {
			topicTitles = loadTopicTitles(topicTitleRoot)
		}
		if topicSources == nil {
			topicSources = loadTopicTitleSources(topicTitleRoot)
		}
		if strings.TrimSpace(topicTitles[topicID]) == "" {
			topicTitles[topicID] = title
			topicSources[topicID] = topicTitleSourceManual
		}
		migratedTopicIDs = noteMigratedSession(migratedTopicIDs, topicID, info.Path, onMigrated)
	}
	if len(migratedTopicIDs) == 0 {
		if !deferred {
			markTopicMigrationDone(dir) // nothing left to migrate — gate future scans
		}
		return repairedTopicIDs
	}
	_ = prependTopicsInProjectsFile(workspaceRoot, migratedTopicIDs, false)
	// Same fresh tombstone re-check as the repair pass: these are whole-map
	// saves, so a concurrent DeleteTopic of an unrelated topic must not have
	// its title written back by this migration batch.
	pruneDeletedTopicEntries(topicTitles, topicSources)
	if topicTitles != nil {
		_ = saveTopicTitles(topicTitleRoot, topicTitles)
	}
	if topicSources != nil {
		_ = saveTopicTitleSources(topicTitleRoot, topicSources)
	}
	invalidateTopicSessionIndex(dir)
	if !deferred {
		markTopicMigrationDone(dir) // pass complete with nothing deferred
	}
	return uniqueStrings(append(repairedTopicIDs, migratedTopicIDs...))
}

// pruneDeletedTopicEntries drops tombstoned topics from scan-built title and
// source maps just before they are persisted, re-reading DeletedTopics so a
// DeleteTopic that landed after the scan snapshot wins. The maps are loaded
// whole at scan start and saved whole at the end; without this re-check the
// save would write the deleted topic's stale entries back, and a title-map
func pruneDeletedTopicEntries(maps ...map[string]string) []string {
	deleted := loadProjectsFile().DeletedTopics
	if len(deleted) == 0 {
		return nil
	}
	for _, m := range maps {
		for _, id := range deleted {
			delete(m, id)
		}
	}
	return deleted
}

func repairIndexedSessionTopicsWithGate(dir string, repairDone func(string) bool) []string {
	if strings.TrimSpace(dir) == "" || repairDone(dir) {
		return nil
	}
	scope, workspaceRoot, topicTitleRoot, ok := legacyMigrationTargetForDir(dir)
	if !ok {
		return nil
	}
	legacyMigrationMu.Lock()
	defer legacyMigrationMu.Unlock()
	if repairDone(dir) {
		return nil
	}
	infos, err := agent.ListSessionOrder(dir)
	if err != nil {
		return nil
	}

	topicTitles, err := loadTopicTitlesForUpdate(topicTitleRoot)
	if err != nil {
		return nil
	}
	topicSources, err := loadTopicTitleSourcesForUpdate(topicTitleRoot)
	if err != nil {
		return nil
	}
	projects := loadProjectsFile()
	deletedTopics := projects.DeletedTopics
	// Repair only topics missing from the sidebar index. Skipping topics that
	// are already listed and titled keeps steady-state rescans write-free:
	// otherwise every rescan (any session activity invalidates the marker)
	indexedTopics := projects.GlobalTopics
	if scope == "project" {
		indexedTopics = nil
		if i := projectIndexByRoot(projects.Projects, workspaceRoot); i >= 0 {
			indexedTopics = projects.Projects[i].Topics
		}
	}
	indexedSet := make(map[string]bool, len(indexedTopics))
	for _, id := range indexedTopics {
		indexedSet[id] = true
	}
	var repairedTopicIDs []string
	var sessionTitles map[string]string
	titlesChanged := false
	sourcesChanged := false
	deferred := false
	for _, info := range infos {
		if sessionOrderInfoIsUnmodifiedRecoveryCopy(info, dir) {
			continue
		}
		topicID := strings.TrimSpace(info.TopicID)
		if topicID == "" {
			continue
		}
		if indexedSet[topicID] && strings.TrimSpace(topicTitles[topicID]) != "" {
			continue // fully indexed already — nothing to repair, skip the meta read
		}
		meta, ok, err := agent.LoadBranchMeta(info.Path)
		if err != nil {
			deferred = true
			continue
		}
		if !ok || strings.TrimSpace(meta.TopicID) == "" {
			continue
		}
		if containsDesktopString(deletedTopics, topicID) {
			continue
		}
		if !legacySessionScopeMatchesMigrationTarget(meta, scope, workspaceRoot) {
			continue
		}
		repairedTopicIDs = append(repairedTopicIDs, topicID)
		if strings.TrimSpace(topicTitles[topicID]) == "" {
			if sessionTitles == nil {
				sessionTitles = loadSessionTitles(dir)
			}
			title := indexedSessionTopicTitle(sessionTitles, info, meta)
			if title == "" {
				title = defaultTopicTitle
			}
			topicTitles[topicID] = title
			titlesChanged = true
		}
		if strings.TrimSpace(topicSources[topicID]) == "" {
			topicSources[topicID] = topicTitleSourceManual
			sourcesChanged = true
		}
	}
	if len(repairedTopicIDs) > 0 {
		// Re-check tombstones right before persisting: a DeleteTopic landing
		// after the scan snapshot must win. The prepend re-filters under the
		// projects-file lock; the whole-map title/source saves and the
		if deletedNow := pruneDeletedTopicEntries(topicTitles, topicSources); len(deletedNow) > 0 {
			deletedSet := make(map[string]bool, len(deletedNow))
			for _, id := range deletedNow {
				deletedSet[id] = true
			}
			live := repairedTopicIDs[:0]
			for _, id := range repairedTopicIDs {
				if !deletedSet[id] {
					live = append(live, id)
				}
			}
			repairedTopicIDs = live
		}
	}
	if len(repairedTopicIDs) > 0 {
		if err := prependTopicsInProjectsFile(workspaceRoot, repairedTopicIDs, false); err != nil {
			deferred = true
		}
		if titlesChanged {
			if err := saveTopicTitles(topicTitleRoot, topicTitles); err != nil {
				deferred = true
			}
		}
		if sourcesChanged {
			if err := saveTopicTitleSources(topicTitleRoot, topicSources); err != nil {
				deferred = true
			}
		}
	}
	if !deferred {
		markTopicIndexRepairDone(dir)
		return uniqueStrings(repairedTopicIDs)
	}
	return nil
}

func indexedSessionTopicTitle(sessionTitles map[string]string, info agent.SessionOrderInfo, meta agent.BranchMeta) string {
	if title := topicTitleFromText(meta.TopicTitle); title != "" {
		return title
	}
	if title := topicTitleFromText(info.TopicTitle); title != "" {
		return title
	}
	if title := topicTitleFromText(sessionTitles[filepath.Base(info.Path)]); title != "" {
		return title
	}
	return topicTitleFromText(info.Preview)
}

func sessionOrderInfoIsAutomaticRecovery(info agent.SessionOrderInfo) bool {
	return info.Recovered ||
		strings.TrimSpace(info.RecoveryDigest) != "" ||
		isAutomaticRecoverySessionPath(info.Path)
}

func sessionInfoIsAutomaticRecovery(info agent.SessionInfo) bool {
	return info.Recovered ||
		strings.TrimSpace(info.RecoveryDigest) != "" ||
		isAutomaticRecoverySessionPath(info.Path)
}

func sessionOrderInfoIsUnmodifiedRecoveryCopy(info agent.SessionOrderInfo, parentDir string) bool {
	return sessionOrderInfoIsAutomaticRecovery(info) &&
		agent.RecoveryBranchCoveredByParent(info.Path, parentDir)
}

func sessionInfoIsUnmodifiedRecoveryCopy(info agent.SessionInfo, parentDir string) bool {
	return sessionInfoIsAutomaticRecovery(info) &&
		agent.RecoveryBranchCoveredByParent(info.Path, parentDir)
}

func isAutomaticRecoverySessionPath(path string) bool {
	id := agent.BranchID(path)
	idx := strings.LastIndex(id, "-recovery-")
	if idx <= 0 {
		return false
	}
	suffix := id[idx+len("-recovery-"):]
	if len(suffix) != 16 {
		return false
	}
	for _, r := range suffix {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func legacyMigrationTargetForDir(dir string) (scope, workspaceRoot, topicTitleRoot string, ok bool) {
	dir = cleanDesktopPath(dir)
	if dir == "" {
		return "", "", "", false
	}
	if sameDesktopPath(dir, config.SessionDir()) || sameDesktopPath(dir, desktopSessionDir(globalWorkspaceRoot())) {
		return "global", "", "", true
	}
	for _, p := range loadProjectsFile().Projects {
		if sameDesktopPath(config.ProjectSessionDir(p.Root), dir) {
			return "project", p.Root, p.Root, true
		}
	}
	return "", "", "", false
}
