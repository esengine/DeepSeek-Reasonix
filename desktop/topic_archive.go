package main

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

var (
	errTopicHasActiveWork = errors.New("wait for the session to finish, answer pending prompts, and stop background jobs before archiving this topic")
	errTopicArchiveBusy   = errors.New("Reasonix is finishing another session change — wait a moment and retry archiving")
)

var topicArchiveCleanupHookForTest func() error

type topicArchiveTrace struct {
	phase        string
	targetCount  int
	runtimeCount int
}

func (a *App) TrashTopic(topicID string) error {
	return friendlySessionFileError(a.trashTopic(topicID))
}

func (a *App) topicHasActiveRuntimeWork(topicID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tabs := range []map[string]*WorkspaceTab{a.tabs, a.detachedSessions} {
		for _, tab := range tabs {
			if tab != nil && tab.TopicID == topicID && tab.hasActiveRuntimeWork() {
				return true
			}
		}
	}
	return false
}

func (a *App) trashTopic(topicID string) (retErr error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return fmt.Errorf("topicID is required")
	}
	started := time.Now()
	trace := topicArchiveTrace{phase: "start"}
	defer func() {
		outcome := "ok"
		if retErr != nil {
			outcome = "failed"
		}
		slog.Debug("desktop: topic archive timing", "outcome", outcome, "phase", trace.phase,
			"total_ms", time.Since(started).Milliseconds(), "target_count", trace.targetCount, "runtime_count", trace.runtimeCount)
	}()
	fallback, changedDirs, err := a.commitTopicArchive(topicID, &trace)
	if err != nil {
		return err
	}
	if fallback.needs {
		trace.phase = "open_fallback"
		fallback.topicID = ""
		if err := a.openFallbackRuntime(fallback); err != nil {
			// Runtime construction errors can include provider configuration
			// details, so keep this recovery diagnostic value-free.
			slog.Warn("desktop: open fallback after topic archive failed")
		}
	}
	trace.phase = "notify"
	if len(changedDirs) > 0 {
		a.emitProjectTreeChangedForSessionDirs(changedDirs...)
	} else {
		a.emitProjectTreeMetadataChanged()
	}
	return nil
}

func (a *App) commitTopicArchive(topicID string, trace *topicArchiveTrace) (fallbackRuntimeTarget, []string, error) {
	trace.phase = "runtime_lock"
	releaseRuntime, ok := a.tryLockRuntimeMutation("trash-topic")
	if !ok {
		return fallbackRuntimeTarget{}, nil, errTopicArchiveBusy
	}
	defer releaseRuntime()
	trace.phase = "removal_lock"
	if !a.sessionRemovalMu.TryLock() {
		return fallbackRuntimeTarget{}, nil, errTopicArchiveBusy
	}
	defer a.sessionRemovalMu.Unlock()
	trace.phase = "active_work_check"
	if a.topicHasActiveRuntimeWork(topicID) {
		return fallbackRuntimeTarget{}, nil, errTopicHasActiveWork
	}
	trace.phase = "target_scan"
	targets, err := a.topicTrashTargets(topicID)
	if err != nil {
		return fallbackRuntimeTarget{}, nil, err
	}
	trace.targetCount = len(targets)
	changedDirs := make([]string, 0, len(targets))
	for _, target := range targets {
		changedDirs = append(changedDirs, target.dir)
	}
	trace.phase = "snapshot"
	removed := a.captureTopicRuntimeBindings(topicID)
	trace.runtimeCount = len(removed)
	if err := a.snapshotTopicRuntimeBindings(removed); err != nil {
		return fallbackRuntimeTarget{}, nil, err
	}
	trace.phase = "acquire_removal_ownership"
	ownership, err := acquireTopicArchiveOwnership(targets, removed)
	if err != nil {
		return fallbackRuntimeTarget{}, nil, err
	}
	defer ownership.release()
	trace.phase = "mark_cleanup_pending"
	rollbackMarkers, err := markTopicArchiveCleanupPending(topicID, targets)
	if err != nil {
		ownership.rollback()
		return fallbackRuntimeTarget{}, nil, err
	}
	trace.phase = "detach_runtimes"
	fallback, unchanged := a.removeTopicRuntimeBindingsIfUnchanged(topicID, removed)
	if !unchanged {
		rollbackMarkers()
		ownership.rollback()
		return fallbackRuntimeTarget{}, nil, errTopicArchiveBusy
	}
	a.finalizeRemovedTopicRuntimes(removed)
	destroyBegun := false
	closedRemoved := map[control.SessionAPI]bool{}
	defer func() {
		if destroyBegun {
			a.closeRemainingRemovedSessionRuntimesAfterDestroyAdmissionHeld(removed, closedRemoved)
		} else {
			a.closeRemainingRemovedSessionRuntimesAdmissionHeld(removed, closedRemoved)
		}
	}()
	trace.phase = "teardown"
	destroyBatches := make([][]control.SessionDestroyHandle, len(targets))
	for i, target := range targets {
		destroys := a.destroyHandlesForSession(target.dir, target.sessionPath, removed)
		destroyBatches[i] = destroys
		destroyBegun = destroyBegun || len(destroys) > 0
	}
	timedOutTargets := waitDestroyHandleBatches(destroyBatches)
	for i, target := range targets {
		destroys := destroyBatches[i]
		a.closeRemovedSessionRuntimesForSessionAfterDestroyAdmissionHeld(removed, target.dir, target.sessionPath, closedRemoved)
		a.removeSessionCatalogPath(target.sessionPath, "topic_archived")
		if timedOutTargets[i] {
			guard := ownership.take(target.sessionPath)
			go delayedDesktopTopicTrash(target.dir, target.sessionPath, target.key, guard, destroys)
			continue
		}
		trace.phase = "move_artifacts"
		var err error
		if hook := topicArchiveCleanupHookForTest; hook != nil {
			err = hook()
		}
		if err == nil {
			err = trashSessionArtifactsWithGuard(target.dir, target.sessionPath, target.key, ownership.take(target.sessionPath))
		}
		finishDestroyHandles(destroys)
		if err != nil {
			// Cleanup-pending is the durable commit point. Once bindings have
			// detached, report the archive as accepted and let startup
			// reconciliation finish any filesystem operation that could not.
			slog.Warn("desktop: topic archive cleanup remains pending")
		}
	}
	trace.phase = "delete_topic_metadata"
	if err := a.deleteTopic(topicID); err != nil {
		slog.Warn("desktop: topic archive metadata cleanup remains pending")
	} else if err := clearTopicArchiveMetadataPending(topicID); err != nil {
		slog.Warn("desktop: topic archive metadata marker cleanup remains pending")
	}
	return fallback, changedDirs, nil
}

func markTopicArchiveCleanupPending(topicID string, targets []topicTrashTarget) (func(), error) {
	if err := markTopicArchiveMetadataPending(topicID, targets); err != nil {
		return nil, err
	}
	marked := make([]string, 0, len(targets))
	rollback := func() {
		for _, path := range marked {
			if err := agent.ClearCleanupPending(path); err != nil {
				slog.Warn("desktop: rollback topic archive marker failed")
			}
		}
		if err := clearTopicArchiveMetadataPending(topicID); err != nil {
			slog.Warn("desktop: rollback topic archive metadata marker failed")
		}
	}
	for _, target := range targets {
		if err := agent.MarkCleanupPending(target.sessionPath, "delete"); err != nil {
			rollback()
			return rollback, err
		}
		marked = append(marked, target.sessionPath)
	}
	return rollback, nil
}

type topicTrashTarget struct {
	dir         string
	sessionPath string
	key         string
}

func (a *App) topicTrashTargets(topicID string) ([]topicTrashTarget, error) {
	topicID = strings.TrimSpace(topicID)
	var targets []topicTrashTarget
	seen := map[string]bool{}
	addTarget := func(dir, path string) error {
		sessionPath, key, err := validateSessionPath(dir, path)
		if err != nil {
			return err
		}
		id := dir + "\x00" + sessionPath
		if seen[id] {
			return nil
		}
		seen[id] = true
		if err := validateSessionTrashTarget(dir, sessionPath, key); err != nil {
			return err
		}
		targets = append(targets, topicTrashTarget{dir: dir, sessionPath: sessionPath, key: key})
		return nil
	}
	for _, dir := range a.knownSessionDirs() {
		index, err := topicSessionIndexForDir(dir)
		if err != nil {
			return nil, err
		}
		for _, match := range index.byTopic[topicID] {
			if !agent.IsCleanupPending(match.path) {
				if err := addTarget(dir, match.path); err != nil {
					return nil, err
				}
			}
		}
	}
	a.mu.RLock()
	var runtimeTargets []struct{ dir, path string }
	for _, tab := range a.runtimeTabsLocked() {
		if tab == nil || tab.TopicID != topicID {
			continue
		}
		if path := canonicalTabSessionPath(tab.currentSessionPath()); path != "" {
			dir := tabSessionDir(tab)
			if filepath.IsAbs(path) {
				dir = filepath.Dir(path)
			}
			runtimeTargets = append(runtimeTargets, struct{ dir, path string }{dir: dir, path: path})
		}
	}
	a.mu.RUnlock()
	for _, target := range runtimeTargets {
		if err := addTarget(target.dir, target.path); err != nil {
			return nil, err
		}
	}
	return targets, nil
}
