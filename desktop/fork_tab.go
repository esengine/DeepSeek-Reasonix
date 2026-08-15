package main

import (
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/agent"
)

const rewindForkAttachError = "conversation fork was created but could not be opened; open the recovery branch from session history"

// openForkedSessionTab attaches an already-written fork session to a new tab.
// The source tab keeps its controller and transcript. The fork becomes active
// only while the source tab still owns focus.
func (a *App) openForkedSessionTab(sourceTab *WorkspaceTab, newPath string) (TabMeta, error) {
	if sourceTab == nil || strings.TrimSpace(newPath) == "" {
		return TabMeta{}, fmt.Errorf("fork tab needs a source tab and session path")
	}
	a.mu.RLock()
	if a.tabs[sourceTab.ID] != sourceTab {
		a.mu.RUnlock()
		return TabMeta{}, nil
	}
	scope := sourceTab.Scope
	workspaceRoot := sourceTab.WorkspaceRoot
	sourceTitle := sourceTab.TopicTitle
	model := sourceTab.model
	effort := cloneStringPtr(sourceTab.effort)
	mode := currentTabMode(sourceTab)
	toolApprovalMode := currentTabToolApprovalMode(sourceTab)
	disabledMCP := cloneServerViewMap(sourceTab.disabledMCP)
	mcpOrder := append([]string(nil), sourceTab.mcpOrder...)
	a.mu.RUnlock()

	topicID := newTopicID()
	topicTitle := a.forkTopicTitle(sourceTitle)
	titleRoot := workspaceRoot
	if scope == "global" {
		titleRoot = ""
	}
	if err := setTopicTitle(titleRoot, topicID, topicTitle); err != nil {
		return TabMeta{}, err
	}
	m, _ := agent.EnsureBranchMeta(newPath)
	m.Scope = scope
	m.WorkspaceRoot = workspaceRoot
	m.TopicID = topicID
	m.TopicTitle = topicTitle
	if err := agent.SaveBranchMeta(newPath, m); err != nil {
		return TabMeta{}, err
	}
	invalidateTopicSessionIndexForPath(newPath)

	a.mu.Lock()
	if a.tabs[sourceTab.ID] != sourceTab {
		a.mu.Unlock()
		return TabMeta{}, nil
	}
	newTabID := a.newUniqueTabIDLocked()
	tab := &WorkspaceTab{
		ID:               newTabID,
		Scope:            scope,
		WorkspaceRoot:    workspaceRoot,
		TopicID:          topicID,
		TopicTitle:       topicTitle,
		topicTitleSource: topicTitleSourceManual,
		SessionPath:      newPath,
		model:            model,
		effort:           effort,
		mode:             mode,
		toolApprovalMode: toolApprovalMode,
		disabledMCP:      disabledMCP,
		mcpOrder:         mcpOrder,
	}
	tab.sink = &tabEventSink{tabID: newTabID, app: a}
	a.tabs[newTabID] = tab
	a.tabOrder = append(a.tabOrder, newTabID)
	activateFork := a.activeTabID == sourceTab.ID
	if activateFork {
		a.activeTabID = newTabID
	}
	a.saveTabsLocked()
	meta := a.tabMeta(tab, activateFork)
	a.mu.Unlock()

	a.emitProjectTreeChangedForSessionDirs(sessionDirectoryForPath(newPath))
	a.startTabControllerBuild(tab)
	return meta, nil
}

// attachForkedRewindTab fails closed when the durable branch cannot be attached
// to a tab. In particular, callers must not treat the source tab as the rewind
// target and accidentally resubmit the edited prompt into the parent session.
func (a *App) attachForkedRewindTab(sourceTab *WorkspaceTab, view RewindResultView) RewindResultView {
	meta, err := a.openForkedSessionTab(sourceTab, view.Branch)
	if err != nil || meta.ID == "" {
		slog.Warn("rewind: fork created but tab attach failed", "err", err)
		view.OK = false
		view.Partial = true
		view.Error = rewindForkAttachError
		return view
	}
	view.TabID = meta.ID
	view.Tab = &meta
	return view
}
