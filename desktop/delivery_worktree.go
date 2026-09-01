package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/workspacelease"
	"reasonix/internal/worktree"
)

var (
	inspectDeliveryWorktree  = worktree.Inspect
	createDeliveryWorktree   = worktree.Create
	rollbackDeliveryWorktree = worktree.RollbackCreate
)

// IsolatedWorktreeOpenResult is returned after an isolated Git workspace has
// been created and opened as a normal Reasonix project.
type IsolatedWorktreeOpenResult struct {
	WorkspaceRoot string  `json:"workspaceRoot"`
	WorktreeRoot  string  `json:"worktreeRoot"`
	SourceRoot    string  `json:"sourceRoot"`
	Branch        string  `json:"branch"`
	SourceDirty   bool    `json:"sourceDirty"`
	Tab           TabMeta `json:"tab"`
}

// DeliveryWorktreeOpenResult is the deprecated alias of
// IsolatedWorktreeOpenResult kept bound for one compatibility version.
type DeliveryWorktreeOpenResult = IsolatedWorktreeOpenResult

// IsolatedWorktreeAvailability reports whether workspaceRoot can use the
// optional Git isolation path. A false result never disables writing itself;
// the cross-platform workspace writer lease remains the no-Git fallback.
func (a *App) IsolatedWorktreeAvailability(workspaceRoot string) worktree.Availability {
	return inspectDeliveryWorktree(a.bootContext(), workspaceRoot)
}

// CreateIsolatedWorktree creates a durable branch-backed worktree and opens it
// as a project. It never switches or modifies the source checkout, and it does
// not delete the new worktree if later UI registration fails. The opened tab
// infers the delivery quality floor (switchable to standard at any time).
func (a *App) CreateIsolatedWorktree(workspaceRoot string) (IsolatedWorktreeOpenResult, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	created, err := createDeliveryWorktree(a.bootContext(), workspaceRoot, config.DeliveryWorktreeDir())
	if err != nil {
		return IsolatedWorktreeOpenResult{}, err
	}

	var tab TabMeta
	if a.singleSurfaceLayoutEnabled() {
		tab, err = a.ensureBlankSurface("project", created.WorkspaceRoot)
	} else {
		tab, err = a.ensureBlankTab("project", created.WorkspaceRoot)
	}
	if err != nil {
		return IsolatedWorktreeOpenResult{}, fmt.Errorf("isolated worktree was created at %s but Reasonix could not open it: %w", created.WorktreeRoot, err)
	}
	return IsolatedWorktreeOpenResult{
		WorkspaceRoot: created.WorkspaceRoot,
		WorktreeRoot:  created.WorktreeRoot,
		SourceRoot:    created.SourceRoot,
		Branch:        created.Branch,
		SourceDirty:   created.SourceDirty,
		Tab:           tab,
	}, nil
}

// DeliveryWorktreeAvailability is the deprecated alias of
// IsolatedWorktreeAvailability, kept bound for one compatibility version.
func (a *App) DeliveryWorktreeAvailability(workspaceRoot string) worktree.Availability {
	return a.IsolatedWorktreeAvailability(workspaceRoot)
}

// CreateDeliveryWorktree is the deprecated alias of CreateIsolatedWorktree,
// kept bound for one compatibility version.
func (a *App) CreateDeliveryWorktree(workspaceRoot string) (DeliveryWorktreeOpenResult, error) {
	return a.CreateIsolatedWorktree(workspaceRoot)
}

var (
	inspectWorktreeMerge  = worktree.InspectMerge
	mergeWorktreeBack     = worktree.MergeBack
	finalizeWorktreeMerge = worktree.FinalizeMerge
)

// MergeWorktreeBackRequest binds a merge to the exact inspection the user
// confirmed. WorkspaceRoot is always resolved from TabID by the backend.
type MergeWorktreeBackRequest struct {
	TabID                string `json:"tabId"`
	ExpectedTargetBranch string `json:"expectedTargetBranch"`
	ExpectedTargetHead   string `json:"expectedTargetHead"`
	ExpectedWorktreeHead string `json:"expectedWorktreeHead"`
	AutoCommitDirty      bool   `json:"autoCommitDirty"`
}

// InspectWorktreeMerge inspects the diff and merge status for the given tab's
// isolated worktree against its base repository branch.
func (a *App) InspectWorktreeMerge(tabID string) (worktree.MergeInspection, error) {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.RUnlock()
		return worktree.MergeInspection{Available: false, Reason: "tab not found", ChangedFiles: []string{}, ConflictFiles: []string{}, Blockers: []worktree.MergeBlocker{}, CleanupBlockers: []worktree.MergeBlocker{}}, a.workspaceNotReadyErr(nil)
	}
	wsRoot, ready, startupErr, ctrl, activity := tab.WorkspaceRoot, tab.Ready, tab.StartupErr, tab.Ctrl, tab.ActivityStatus
	a.mu.RUnlock()
	inspection, err := inspectWorktreeMerge(a.bootContext(), wsRoot, config.DeliveryWorktreeDir())
	if err != nil {
		return inspection, err
	}
	if !ready || ctrl == nil || strings.TrimSpace(startupErr) != "" {
		inspection.CanMerge = false
		inspection.Blockers = append(inspection.Blockers, worktree.MergeBlocker{Code: "tab_building", Message: "the worktree tab is still building or unavailable", Paths: []string{}})
	}
	if activeWorkForController(ctrl).active() || mergeActivityActive(activity) {
		inspection.CanMerge = false
		inspection.Blockers = append(inspection.Blockers, worktree.MergeBlocker{Code: "active_work", Message: "the worktree tab still has active or waiting work", Paths: []string{}})
	}
	return inspection, nil
}

// MergeWorktreeBack merges only after active-work and dual-workspace lease
// gates. It intentionally leaves navigation, tab closure, and cleanup to the
// second phase.
func (a *App) MergeWorktreeBack(request MergeWorktreeBackRequest) (worktree.MergeResult, error) {
	a.worktreeMergeMu.Lock()
	defer a.worktreeMergeMu.Unlock()

	tab, wsRoot, err := a.mergeableWorktreeTab(request.TabID)
	if err != nil {
		return worktree.MergeResult{Error: err.Error()}, err
	}
	inspection, err := inspectWorktreeMerge(a.bootContext(), wsRoot, config.DeliveryWorktreeDir())
	if err != nil {
		return worktree.MergeResult{Error: err.Error()}, err
	}
	release, err := holdWorktreeMergeLeases(a.bootContext(), inspection.SourceRoot, inspection.WorktreeRoot)
	if err != nil {
		return worktree.MergeResult{Error: err.Error()}, err
	}
	defer release()
	if _, currentRoot, err := a.mergeableWorktreeTabIdentity(request.TabID, tab); err != nil || !sameProjectRoot(currentRoot, wsRoot) {
		if err == nil {
			err = fmt.Errorf("worktree tab identity changed while waiting for merge access")
		}
		return worktree.MergeResult{Error: err.Error()}, err
	}
	return mergeWorktreeBack(a.bootContext(), config.DeliveryWorktreeDir(), worktree.MergeRequest{
		WorkspaceRoot: wsRoot, ExpectedTargetBranch: request.ExpectedTargetBranch,
		ExpectedTargetHead: request.ExpectedTargetHead, ExpectedWorktreeHead: request.ExpectedWorktreeHead,
		AutoCommitDirty: request.AutoCommitDirty,
	})
}

// FinalizeWorktreeMerge is the cleanup phase. The frontend calls it only after
// navigating to source and closing the worktree view; the backend proves no
// visible or detached runtime still references the allocation.
func (a *App) FinalizeWorktreeMerge(request worktree.CleanupRequest) (worktree.CleanupResult, error) {
	a.worktreeMergeMu.Lock()
	defer a.worktreeMergeMu.Unlock()
	if a.worktreeRuntimeReferenced(request.WorktreeRoot) {
		err := fmt.Errorf("a visible or background runtime still references the worktree; it was preserved")
		return worktree.CleanupResult{Blockers: []worktree.MergeBlocker{{Code: "runtime_reference", Message: err.Error(), Paths: []string{}}}, Error: err.Error()}, err
	}
	release, err := holdWorktreeMergeLeases(a.bootContext(), request.SourceRoot, request.WorktreeRoot)
	if err != nil {
		return worktree.CleanupResult{Blockers: []worktree.MergeBlocker{}, Error: err.Error()}, err
	}
	defer release()
	if a.worktreeRuntimeReferenced(request.WorktreeRoot) {
		err := fmt.Errorf("a runtime started referencing the worktree while cleanup waited; it was preserved")
		return worktree.CleanupResult{Blockers: []worktree.MergeBlocker{{Code: "runtime_reference", Message: err.Error(), Paths: []string{}}}, Error: err.Error()}, err
	}
	result, err := finalizeWorktreeMerge(a.bootContext(), config.DeliveryWorktreeDir(), request)
	if result.Completed {
		a.emitProjectTreeChanged()
	}
	return result, err
}

func (a *App) mergeableWorktreeTab(tabID string) (*WorkspaceTab, string, error) {
	return a.mergeableWorktreeTabIdentity(tabID, nil)
}

func (a *App) mergeableWorktreeTabIdentity(tabID string, expected *WorkspaceTab) (*WorkspaceTab, string, error) {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil || (expected != nil && tab != expected) {
		a.mu.RUnlock()
		return nil, "", fmt.Errorf("worktree tab was closed or replaced")
	}
	root, ready, startupErr, ctrl, activity := tab.WorkspaceRoot, tab.Ready, tab.StartupErr, tab.Ctrl, tab.ActivityStatus
	a.mu.RUnlock()
	if !ready || ctrl == nil || strings.TrimSpace(startupErr) != "" {
		return nil, "", fmt.Errorf("worktree tab is still building or unavailable")
	}
	if activeWorkForController(ctrl).active() || mergeActivityActive(activity) {
		return nil, "", fmt.Errorf("worktree tab has active, waiting, or background work")
	}
	return tab, root, nil
}

func mergeActivityActive(status string) bool {
	switch strings.TrimSpace(status) {
	case topicStatusThinking, topicStatusStreaming, topicStatusWaitingConfirmation, topicStatusBackgroundJob:
		return true
	default:
		return false
	}
}

func holdWorktreeMergeLeases(parent context.Context, roots ...string) (func(), error) {
	type leaseRoot struct{ canonical, root string }
	unique := map[string]string{}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		canonical, err := workspacelease.CanonicalWorkspace(root)
		if err != nil {
			return nil, fmt.Errorf("resolve merge workspace lease: %w", err)
		}
		unique[canonical] = root
	}
	ordered := make([]leaseRoot, 0, len(unique))
	for canonical, root := range unique {
		ordered = append(ordered, leaseRoot{canonical: canonical, root: root})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].canonical < ordered[j].canonical })
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	releases := make([]func(), 0, len(ordered))
	for _, item := range ordered {
		owner, err := workspacelease.New(item.root, config.WorkspaceLeaseDir(), nil)
		if err != nil {
			cancel()
			runReleasesReverse(releases)
			return nil, fmt.Errorf("create merge workspace lease: %w", err)
		}
		release, err := owner.HoldWrite(ctx)
		if err != nil {
			cancel()
			runReleasesReverse(releases)
			return nil, fmt.Errorf("wait for merge workspace lease: %w", err)
		}
		releases = append(releases, release)
	}
	return func() { runReleasesReverse(releases); cancel() }, nil
}

func runReleasesReverse(releases []func()) {
	for index := len(releases) - 1; index >= 0; index-- {
		releases[index]()
	}
}

func (a *App) worktreeRuntimeReferenced(worktreeRoot string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.runtimeTabsLocked() {
		if tab != nil && pathWithinWorktree(tab.WorkspaceRoot, worktreeRoot) {
			return true
		}
	}
	return false
}

func pathWithinWorktree(path, worktreeRoot string) bool {
	path = normalizeProjectRoot(path)
	worktreeRoot = normalizeProjectRoot(worktreeRoot)
	if path == "" || worktreeRoot == "" {
		return false
	}
	if sameProjectRoot(path, worktreeRoot) {
		return true
	}
	rel, err := filepath.Rel(worktreeRoot, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
