package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	TabID                      string `json:"tabId"`
	ExpectedTargetBranch       string `json:"expectedTargetBranch"`
	ExpectedTargetHead         string `json:"expectedTargetHead"`
	ExpectedWorktreeHead       string `json:"expectedWorktreeHead"`
	ExpectedWorktreeStateToken string `json:"expectedWorktreeStateToken"`
	AutoCommitDirty            bool   `json:"autoCommitDirty"`
}

// CloseMergedWorktreeTabRequest binds the lifecycle handoff to both the source
// and worktree identities observed by the frontend after navigation.
type CloseMergedWorktreeTabRequest struct {
	TabID        string `json:"tabId"`
	WorktreeRoot string `json:"worktreeRoot"`
	SourceTabID  string `json:"sourceTabId"`
	SourceRoot   string `json:"sourceRoot"`
}

type CloseMergedWorktreeTabResult struct {
	Closed     bool `json:"closed"`
	Idempotent bool `json:"idempotent"`
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
		ExpectedWorktreeStateToken: request.ExpectedWorktreeStateToken,
		AutoCommitDirty:            request.AutoCommitDirty,
	})
}

// FinalizeWorktreeMerge is the cleanup phase. The frontend calls it only after
// navigating to source and closing the worktree view; the backend proves no
// visible or detached runtime still references the allocation.
func (a *App) FinalizeWorktreeMerge(request worktree.CleanupRequest) (worktree.CleanupResult, error) {
	a.worktreeMergeMu.Lock()
	defer a.worktreeMergeMu.Unlock()
	releaseReservation, err := a.reserveWorktreeCleanup(request.WorktreeRoot)
	if err != nil {
		return worktree.CleanupResult{Blockers: []worktree.MergeBlocker{{Code: "runtime_reference", Message: err.Error(), Paths: []string{}}}, Error: err.Error()}, err
	}
	defer releaseReservation()
	release, err := holdWorktreeMergeLeases(a.bootContext(), request.SourceRoot, request.WorktreeRoot)
	if err != nil {
		return worktree.CleanupResult{Blockers: []worktree.MergeBlocker{}, Error: err.Error()}, err
	}
	defer release()
	if a.worktreeRuntimeReferenced(request.WorktreeRoot) {
		err := fmt.Errorf("a runtime still references the reserved worktree; it was preserved")
		return worktree.CleanupResult{Blockers: []worktree.MergeBlocker{{Code: "runtime_reference", Message: err.Error(), Paths: []string{}}}, Error: err.Error()}, err
	}
	result, err := finalizeWorktreeMerge(a.bootContext(), config.DeliveryWorktreeDir(), request)
	if result.Completed {
		a.emitProjectTreeChanged()
	}
	return result, err
}

// CloseMergedWorktreeTab closes only the exact idle worktree view after the
// exact source tab is active. It rechecks the predicate under App.mu at the
// removal point; an already-pruned single-surface worktree is idempotent only
// when no detached runtime references it.
func (a *App) CloseMergedWorktreeTab(request CloseMergedWorktreeTabRequest) (CloseMergedWorktreeTabResult, error) {
	worktreeKey, err := workspacelease.CanonicalWorkspace(request.WorktreeRoot)
	if err != nil {
		return CloseMergedWorktreeTabResult{}, fmt.Errorf("resolve worktree identity: %w", err)
	}
	sourceKey, err := workspacelease.CanonicalWorkspace(request.SourceRoot)
	if err != nil {
		return CloseMergedWorktreeTabResult{}, fmt.Errorf("resolve source identity: %w", err)
	}
	defer a.lockRuntimeMutation("close-merged-worktree-tab")()
	a.sessionRemovalMu.Lock()
	defer a.sessionRemovalMu.Unlock()

	a.mu.Lock()
	tab, err := a.validateMergedWorktreeCloseLocked(request, worktreeKey, sourceKey)
	if err != nil {
		a.mu.Unlock()
		return CloseMergedWorktreeTabResult{}, err
	}
	if tab == nil {
		a.mu.Unlock()
		return CloseMergedWorktreeTabResult{Closed: true, Idempotent: true}, nil
	}
	a.mu.Unlock()
	if err := a.snapshotTab(tab); err != nil {
		return CloseMergedWorktreeTabResult{}, fmt.Errorf("save worktree session before closing: %w", err)
	}
	if err := a.saveTabSessionMetaForCurrentSession(tab); err != nil {
		return CloseMergedWorktreeTabResult{}, fmt.Errorf("save worktree session metadata before closing: %w", err)
	}

	a.mu.Lock()
	current, err := a.validateMergedWorktreeCloseLocked(request, worktreeKey, sourceKey)
	if err != nil {
		a.mu.Unlock()
		return CloseMergedWorktreeTabResult{}, err
	}
	if current != tab {
		a.mu.Unlock()
		return CloseMergedWorktreeTabResult{}, fmt.Errorf("worktree tab changed before close; resources were preserved")
	}
	a.markTabRemovedLocked(tab)
	delete(a.tabs, tab.ID)
	a.removeTabOrderLocked(tab.ID)
	a.saveTabsLocked()
	a.mu.Unlock()

	if a.terminals != nil {
		a.terminals.closeForTab(tab.ID)
	}
	a.closeTabRuntimeAdmissionHeld(tab)
	if a.workspaceHub != nil {
		a.workspaceHub.reconcileRoots()
	}
	a.emitProjectTreeRuntimeChangedWithLegacy()
	return CloseMergedWorktreeTabResult{Closed: true}, nil
}

func (a *App) validateMergedWorktreeCloseLocked(request CloseMergedWorktreeTabRequest, worktreeKey, sourceKey string) (*WorkspaceTab, error) {
	if request.TabID == "" || request.SourceTabID == "" || request.TabID == request.SourceTabID {
		return nil, fmt.Errorf("merged worktree close identity is incomplete")
	}
	source := a.tabs[request.SourceTabID]
	if source == nil || a.activeTabID != source.ID || canonicalRuntimeRoot(source.WorkspaceRoot) != sourceKey {
		return nil, fmt.Errorf("source tab is no longer the active recorded workspace; resources were preserved")
	}
	tab := a.tabs[request.TabID]
	if tab == nil {
		if a.runtimeReferencesCanonicalLocked(worktreeKey) {
			return nil, fmt.Errorf("a detached runtime still references the worktree; resources were preserved")
		}
		return nil, nil
	}
	if canonicalRuntimeRoot(tab.WorkspaceRoot) != worktreeKey {
		return nil, fmt.Errorf("worktree tab identity changed; resources were preserved")
	}
	if tab.hasActiveRuntimeWork() || mergeActivityActive(tab.ActivityStatus) {
		return nil, fmt.Errorf("worktree tab is no longer idle; resources were preserved")
	}
	return tab, nil
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
	key, err := workspacelease.CanonicalWorkspace(worktreeRoot)
	if err != nil {
		return true
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.runtimeReferencesCanonicalLocked(key)
}

func pathWithinWorktree(path, worktreeRoot string) bool {
	pathKey := canonicalRuntimeRoot(path)
	rootKey := canonicalRuntimeRoot(worktreeRoot)
	if pathKey == "" || rootKey == "" {
		return false
	}
	if pathKey == rootKey {
		return true
	}
	rel, err := filepath.Rel(rootKey, pathKey)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func canonicalRuntimeRoot(root string) string {
	canonical, _ := canonicalRuntimeRootErr(root)
	return canonical
}

func canonicalRuntimeRootErr(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("workspace root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	probe := filepath.Clean(abs)
	suffix := []string{}
	var probeInfo os.FileInfo
	for {
		if info, statErr := os.Lstat(probe); statErr == nil {
			probeInfo = info
			break
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
	if resolved, resolveErr := filepath.EvalSymlinks(probe); resolveErr == nil {
		probe = resolved
	} else if errors.Is(resolveErr, os.ErrNotExist) && probeInfo != nil && probeInfo.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(probe)
		if readErr != nil {
			return "", readErr
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(probe), target)
		}
		probe, readErr = canonicalRuntimeRootErr(filepath.Clean(target))
		if readErr != nil {
			return "", readErr
		}
	} else if !os.IsNotExist(resolveErr) {
		return "", resolveErr
	}
	for index := len(suffix) - 1; index >= 0; index-- {
		probe = filepath.Join(probe, suffix[index])
	}
	return workspacelease.CanonicalWorkspace(probe)
}

func (a *App) runtimeReferencesCanonicalLocked(worktreeKey string) bool {
	if worktreeKey == "" {
		return true
	}
	for _, tab := range a.runtimeTabsLocked() {
		if tab != nil && pathWithinCanonicalWorktree(canonicalRuntimeRoot(tab.WorkspaceRoot), worktreeKey) {
			return true
		}
	}
	return false
}

func pathWithinCanonicalWorktree(pathKey, worktreeKey string) bool {
	if pathKey == "" || worktreeKey == "" {
		return false
	}
	if pathKey == worktreeKey {
		return true
	}
	rel, err := filepath.Rel(worktreeKey, pathKey)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (a *App) workspaceCleanupReservedLocked(workspaceKey string) bool {
	for reservedRoot := range a.worktreeCleanupReservations {
		if pathWithinCanonicalWorktree(workspaceKey, reservedRoot) {
			return true
		}
	}
	return false
}

func (a *App) cleanupReservationOverlapsLocked(worktreeKey string) bool {
	for reservedRoot := range a.worktreeCleanupReservations {
		if pathWithinCanonicalWorktree(worktreeKey, reservedRoot) || pathWithinCanonicalWorktree(reservedRoot, worktreeKey) {
			return true
		}
	}
	return false
}

func (a *App) reserveWorktreeCleanup(worktreeRoot string) (func(), error) {
	key, err := canonicalRuntimeRootErr(worktreeRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve cleanup worktree identity: %w", err)
	}
	a.worktreeCleanupMu.Lock()
	if a.worktreeCleanupReservations == nil {
		a.worktreeCleanupReservations = map[string]struct{}{}
	}
	if a.cleanupReservationOverlapsLocked(key) {
		a.worktreeCleanupMu.Unlock()
		return nil, fmt.Errorf("worktree cleanup is already in progress")
	}
	a.mu.RLock()
	referenced := a.runtimeReferencesCanonicalLocked(key)
	if !referenced {
		a.worktreeCleanupReservations[key] = struct{}{}
	}
	a.mu.RUnlock()
	a.worktreeCleanupMu.Unlock()
	if referenced {
		return nil, fmt.Errorf("a visible or background runtime still references the worktree; it was preserved")
	}
	return func() {
		a.worktreeCleanupMu.Lock()
		delete(a.worktreeCleanupReservations, key)
		a.worktreeCleanupMu.Unlock()
	}, nil
}

// beginWorkspaceRuntimeAdmission holds the cleanup-reservation gate through a
// runtime owner's final App.mu publication. Callers must invoke it before
// acquiring App.mu and defer the returned release.
func (a *App) beginWorkspaceRuntimeAdmission(workspaceRoot string) (func(), error) {
	key, err := canonicalRuntimeRootErr(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime workspace identity: %w", err)
	}
	a.worktreeCleanupMu.Lock()
	if a.workspaceCleanupReservedLocked(key) {
		a.worktreeCleanupMu.Unlock()
		return nil, fmt.Errorf("workspace cleanup is in progress; retry after cleanup completes")
	}
	return a.worktreeCleanupMu.Unlock, nil
}
