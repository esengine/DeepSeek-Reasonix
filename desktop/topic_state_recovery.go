package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/topicstate"
)

func (m *topicStateManager) openTopicStoreWithRecovery(ctx context.Context, scope *topicStateScope) (*topicstate.Store, error) {
	store, err := topicstate.Open(ctx, scope.path)
	if err == nil || !topicstate.IsCorruptionError(err) {
		return store, err
	}
	recovered, recoveryErr := recoverTopicRecordsFromSessions(scope.root)
	hasLegacy := legacyTopicFilesExist(scope.root)
	if recoveryErr != nil {
		slog.Warn("desktop: topic session recovery source incomplete", "scope", topicScopeKind(scope.root), "error_type", topicStateErrorType(recoveryErr))
	}
	if !hasLegacy && len(recovered) == 0 {
		return nil, err
	}
	quarantined, quarantineErr := topicstate.Quarantine(scope.path, m.now())
	if quarantineErr != nil {
		return nil, fmt.Errorf("preserve corrupt topic database: %w", quarantineErr)
	}
	slog.Warn("desktop: rebuilt corrupt topic state", "scope", topicScopeKind(scope.root), "records", len(recovered), "legacy", hasLegacy, "quarantined", filepath.Base(quarantined))
	store, err = topicstate.Open(ctx, scope.path)
	if err != nil || len(recovered) == 0 {
		return store, err
	}
	if _, err := store.ReplaceAll(ctx, recovered); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

// recoverTopicRecordsFromSessions reconstructs only metadata already carried
// by valid branch sidecars. It is used after a quick_check failure, before the
// corrupt database is quarantined, and never turns transcript text into a new
// authority source.
func recoverTopicRecordsFromSessions(workspaceRoot string) (map[string]topicstate.Record, error) {
	workspaceRoot = normalizeProjectRoot(workspaceRoot)
	dirs := topicRecoverySessionDirs(workspaceRoot)
	deleted := deletedTopicSet()
	records := map[string]topicstate.Record{}
	var recoveryErr error
	for _, dir := range dirs {
		infos, err := agent.ListSessionOrder(dir)
		if err != nil {
			recoveryErr = errors.Join(recoveryErr, err)
			continue
		}
		sessionTitles := loadSessionTitles(dir)
		for _, info := range infos {
			topicID := strings.TrimSpace(info.TopicID)
			if topicID == "" || deleted[topicID] || !topicRecoveryInfoMatchesScope(info, workspaceRoot) {
				continue
			}
			title := topicTitleFromText(info.TopicTitle)
			if title == "" {
				title = topicTitleFromText(sessionTitles[filepath.Base(info.Path)])
			}
			if title == "" {
				continue
			}
			record := records[topicID]
			record.TopicID = topicID
			if record.Title == "" || isDefaultTopicTitle(record.Title) {
				record.Title = agent.UserPreviewText(title)
				record.TitleSource = topicTitleSourceManual
			}
			createdAt := info.CreatedAt.UnixMilli()
			if createdAt > 0 && (record.CreatedAtMS == 0 || createdAt < record.CreatedAtMS) {
				record.CreatedAtMS = createdAt
			}
			records[topicID] = record
		}
	}
	return records, recoveryErr
}

func topicRecoverySessionDirs(workspaceRoot string) []string {
	if workspaceRoot != "" {
		return []string{desktopSessionDir(workspaceRoot)}
	}
	dirs := []string{config.SessionDir(), desktopSessionDir(globalWorkspaceRoot())}
	if sameDesktopPath(dirs[0], dirs[1]) {
		return dirs[:1]
	}
	return dirs
}

func topicRecoveryInfoMatchesScope(info agent.SessionOrderInfo, workspaceRoot string) bool {
	if workspaceRoot == "" {
		return strings.TrimSpace(info.Scope) != "project" && strings.TrimSpace(info.WorkspaceRoot) == ""
	}
	return strings.TrimSpace(info.Scope) == "project" && sameProjectRoot(info.WorkspaceRoot, workspaceRoot)
}
