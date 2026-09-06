package main

import (
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

func (a *App) handleTabSessionRecovered(tab *WorkspaceTab) func(control.SessionRecoveryInfo) error {
	return func(info control.SessionRecoveryInfo) error {
		if strings.TrimSpace(info.RecoveryPath) == "" {
			return nil
		}
		if err := copyPinnedContextState(info.OriginalPath, info.RecoveryPath); err != nil {
			return fmt.Errorf("copy pinned context to recovery session: %w", err)
		}
		pinnedState, err := loadPinnedContextState(info.RecoveryPath)
		if err != nil {
			return fmt.Errorf("load recovery pinned context: %w", err)
		}
		if err := a.handoffTabRecoveryLease(tab, info.RecoveryPath); err != nil {
			return err
		}
		meta := info.Meta
		scope := strings.TrimSpace(meta.Scope)
		if scope != "project" {
			scope = "global"
		}
		workspaceRoot := strings.TrimSpace(meta.WorkspaceRoot)
		if scope == "global" {
			workspaceRoot = ""
		}
		invalidateTopicSessionIndexForPath(info.RecoveryPath)
		a.mu.Lock()
		if tab != nil && !tab.removed {
			oldKey := sessionRuntimeKey(info.OriginalPath)
			newKey := sessionRuntimeKey(info.RecoveryPath)
			if oldKey != "" && newKey != "" && a.detachedSessions[oldKey] == tab {
				delete(a.detachedSessions, oldKey)
				a.ensureDetachedSessionsLocked()
				a.detachedSessions[newKey] = tab
			}
			tab.SessionPath = canonicalTabSessionPath(info.RecoveryPath)
			if a.tabs[tab.ID] == tab {
				a.saveTabsLocked()
			}
		}
		a.mu.Unlock()
		info.OnCommit(func() {
			if tab != nil {
				tab.setPinnedFiles(pinnedState.Files)
			}
		})
		// The fork continues the same conversation, so its cost history moves
		// with it: re-key the in-memory telemetry from the original session to
		// the recovery path and persist the sidecar right away.
		if tab != nil && !tab.removed {
			origKey := sessionRuntimeKey(info.OriginalPath)
			newKey := sessionRuntimeKey(info.RecoveryPath)
			carried := false
			if newKey != "" {
				tab.telemMu.Lock()
				if tab.telemetrySessionKey == origKey || tab.telemetrySessionKey == "" {
					tab.telemetrySessionKey = newKey
					carried = true
				}
				tab.telemMu.Unlock()
			}
			if carried {
				_ = saveTelemetry(info.RecoveryPath+".telemetry.json", tab.telemetrySnapshot())
			}
		}
		a.emitSessionRecoveredAndRefresh(sessionDirectoryForPath(info.RecoveryPath), sessionRecoveryEvent{
			ConversationID:    meta.TopicID,
			ActiveVersionID:   agent.BranchID(info.RecoveryPath),
			RecoveryVersionID: agent.BranchID(info.RecoveryPath),
			OriginalPath:      info.OriginalPath,
			RecoveryPath:      info.RecoveryPath,
			Scope:             scope,
			WorkspaceRoot:     workspaceRoot,
			TopicID:           meta.TopicID,
			TopicTitle:        meta.TopicTitle,
			RecoveryReason:    meta.RecoveryReason,
			RecoveryDigest:    meta.RecoveryDigest,
			RecoveryParentID:  string(meta.ParentID),
			Existing:          info.Existing,
			BaseRevision:      info.BaseRevision,
			DiskRevision:      info.DiskRevision,
			CanContinue:       true,
			RequiresChoice:    false,
		})
		a.invalidatePromptHistoryCache()
		return nil
	}
}
