package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/worktree"
)

const rewindForkAttachError = "conversation fork was created but could not be opened; open the recovery branch from session history"

// forkTabBeforePublishHookForTest forces the persistence-to-publish interleaving.
var forkTabBeforePublishHookForTest atomic.Pointer[func()]

type forkedSessionTabOpen struct {
	tab                 TabMeta
	workspaceReferenced bool
}

// ForkWorktreeResultView distinguishes a real isolated fork from a safe shared
// fallback and from a dirty-source refusal. The ordinary ForkForTab contract is
// intentionally unchanged for embedded frontend/backend compatibility.
type ForkWorktreeResultView struct {
	Tab              TabMeta `json:"tab"`
	Isolated         bool    `json:"isolated"`
	FallbackToShared bool    `json:"fallbackToShared,omitempty"`
	SourceDirty      bool    `json:"sourceDirty,omitempty"`
	Branch           string  `json:"branch,omitempty"`
}

// forkForTabWithOptions forks the requested source tab, optionally creating an
// isolated Git worktree for the new tab so changes in the fork do not mutate the
// source workspace.
func (a *App) forkForTabWithOptions(tabID string, turn int, isolateWorkspace bool) (ForkWorktreeResultView, error) {
	sourceTab, ctrl := a.tabAndCtrlByID(tabID)
	if sourceTab == nil || ctrl == nil {
		return ForkWorktreeResultView{}, nil
	}
	if a.tabIsReadOnly(sourceTab) {
		return ForkWorktreeResultView{}, readOnlyChannelErr()
	}
	if err := a.ensureTabControllerWorkspace(sourceTab); err != nil {
		return ForkWorktreeResultView{}, err
	}
	a.mu.RLock()
	if a.tabs[sourceTab.ID] != sourceTab || sourceTab.Ctrl == nil {
		a.mu.RUnlock()
		return ForkWorktreeResultView{}, nil
	}
	ctrl = sourceTab.Ctrl
	scope := sourceTab.Scope
	srcRoot := sourceTab.WorkspaceRoot
	a.mu.RUnlock()

	result := ForkWorktreeResultView{}
	var created worktree.Result
	if isolateWorkspace {
		if scope != "project" || strings.TrimSpace(srcRoot) == "" {
			result.FallbackToShared = true
		} else {
			avail := inspectDeliveryWorktree(a.bootContext(), srcRoot)
			if !avail.Available {
				result.FallbackToShared = true
			} else if avail.SourceDirty {
				result.SourceDirty = true
				return result, nil
			} else {
				var err error
				created, err = createDeliveryWorktree(a.bootContext(), srcRoot, config.DeliveryWorktreeDir())
				if err != nil {
					return ForkWorktreeResultView{}, fmt.Errorf("create isolated fork worktree: %w", err)
				}
				if created.SourceDirty {
					if rollbackErr := rollbackDeliveryWorktree(a.bootContext(), created); rollbackErr != nil {
						return ForkWorktreeResultView{}, fmt.Errorf("source changed while creating isolated worktree at %s; automatic cleanup failed: %w", created.WorktreeRoot, rollbackErr)
					}
					result.SourceDirty = true
					return result, nil
				}
				result.Isolated = true
				result.Branch = created.Branch
			}
		}
	}

	newPath, err := ctrl.ForkSession(turn, "")
	if err != nil {
		return ForkWorktreeResultView{}, a.rollbackUnusedForkWorktree(created, err)
	}
	opened, err := a.openForkedSessionTabWithWorkspace(sourceTab, newPath, created.WorkspaceRoot)
	result.Tab = opened.tab
	if err != nil {
		if opened.workspaceReferenced {
			return result, err
		}
		return ForkWorktreeResultView{}, a.rollbackUnusedForkWorktree(created, err)
	}
	if result.Tab.ID == "" {
		if opened.workspaceReferenced {
			return result, errors.New(rewindForkAttachError)
		}
		return ForkWorktreeResultView{}, a.rollbackUnusedForkWorktree(created, errors.New(rewindForkAttachError))
	}
	return result, nil
}

func (a *App) rollbackUnusedForkWorktree(created worktree.Result, cause error) error {
	if strings.TrimSpace(created.WorktreeRoot) == "" {
		return cause
	}
	if err := rollbackDeliveryWorktree(a.bootContext(), created); err != nil {
		return errors.Join(cause, fmt.Errorf("preserve unused isolated worktree at %s after cleanup failed: %w", created.WorktreeRoot, err))
	}
	return cause
}

// openForkedSessionTab attaches an already-written fork session to a new tab.
// The source tab keeps its controller and transcript. The fork becomes active
// only while the source tab still owns focus.
func (a *App) openForkedSessionTab(sourceTab *WorkspaceTab, newPath string) (TabMeta, error) {
	opened, err := a.openForkedSessionTabWithWorkspace(sourceTab, newPath, "")
	return opened.tab, err
}

// openForkedSessionTabWithWorkspace attaches an already-written fork session to a new tab,
// optionally overriding the workspace root (e.g. for isolated Git worktrees).
func (a *App) openForkedSessionTabWithWorkspace(sourceTab *WorkspaceTab, newPath string, workspaceRootOverride string) (forkedSessionTabOpen, error) {
	if sourceTab == nil || strings.TrimSpace(newPath) == "" {
		return forkedSessionTabOpen{}, fmt.Errorf("fork tab needs a source tab and session path")
	}
	a.mu.RLock()
	if a.tabs[sourceTab.ID] != sourceTab {
		a.mu.RUnlock()
		return forkedSessionTabOpen{}, nil
	}
	scope := sourceTab.Scope
	workspaceRoot := sourceTab.WorkspaceRoot
	if strings.TrimSpace(workspaceRootOverride) != "" {
		workspaceRoot = workspaceRootOverride
	}
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
		return forkedSessionTabOpen{}, err
	}
	m, _ := agent.EnsureBranchMeta(newPath)
	m.Scope = scope
	m.WorkspaceRoot = workspaceRoot
	m.TopicID = topicID
	m.TopicTitle = topicTitle
	if err := agent.SaveBranchMeta(newPath, m); err != nil {
		return forkedSessionTabOpen{}, err
	}
	invalidateTopicSessionIndexForPath(newPath)
	opened := forkedSessionTabOpen{workspaceReferenced: strings.TrimSpace(workspaceRootOverride) != ""}

	if opened.workspaceReferenced && scope == "project" {
		rememberWorkspace(workspaceRoot)
		if err := prependTopicInProjectsFile(workspaceRoot, topicID, true); err != nil {
			slog.Warn("desktop: persist isolated fork topic", "workspace", workspaceRoot, "topic", topicID, "err", err)
		}
		a.registerProjectRoot(workspaceRoot)
	}
	if hook := forkTabBeforePublishHookForTest.Load(); hook != nil {
		(*hook)()
	}

	a.mu.Lock()
	if a.tabs[sourceTab.ID] != sourceTab {
		a.mu.Unlock()
		return opened, nil
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

	if opened.workspaceReferenced && scope == "project" {
		if activateFork {
			saveWorkspace(workspaceRoot)
		}
	}
	a.emitProjectTreeChangedForSessionDirs(sessionDirectoryForPath(newPath))
	a.startTabControllerBuild(tab)
	opened.tab = meta
	return opened, nil
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
