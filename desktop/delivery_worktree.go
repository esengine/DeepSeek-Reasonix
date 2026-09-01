package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	created, err := func() (worktree.Result, error) {
		releaseAdmission, err := a.beginWorkspaceRuntimeAdmission(workspaceRoot)
		if err != nil {
			return worktree.Result{}, err
		}
		defer releaseAdmission()
		return createDeliveryWorktree(a.bootContext(), workspaceRoot, config.DeliveryWorktreeDir())
	}()
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
	removeWorktreeProject = removeProject
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
	TabID                 string `json:"tabId"`
	WorktreeRoot          string `json:"worktreeRoot"`
	SourceTabID           string `json:"sourceTabId"`
	SourceRoot            string `json:"sourceRoot"`
	NavigationIntentToken string `json:"navigationIntentToken"`
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
	wsRoot := tab.WorkspaceRoot
	a.mu.RUnlock()
	inspection, err := inspectWorktreeMerge(a.bootContext(), wsRoot, config.DeliveryWorktreeDir())
	if err != nil {
		return inspection, err
	}
	if blockers := a.inspectWorktreeMergeRuntimeBlockers(inspection.SourceRoot, inspection.WorktreeRoot); len(blockers) > 0 {
		inspection.CanMerge = false
		inspection.Blockers = append(inspection.Blockers, blockers...)
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
	if blockers := a.inspectWorktreeMergeRuntimeBlockers(inspection.SourceRoot, inspection.WorktreeRoot); len(blockers) > 0 {
		err := mergeRuntimeBlockersError(blockers)
		return worktree.MergeResult{Error: err.Error()}, err
	}
	release, err := holdWorktreeMergeLeases(a.bootContext(), inspection.SourceRoot, inspection.WorktreeRoot)
	if err != nil {
		return worktree.MergeResult{Error: err.Error()}, err
	}
	defer release()
	releaseReservation, err := a.reserveWorktreeMergeRuntime(inspection.SourceRoot, inspection.WorktreeRoot)
	if err != nil {
		return worktree.MergeResult{Error: err.Error()}, err
	}
	defer releaseReservation()
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
	if err != nil && !result.RecoveryRetained {
		return result, err
	}
	if result.Completed || result.RecoveryRetained {
		if err := a.forgetFinalizedWorktreeProject(request); err != nil {
			result.Error = err.Error()
			return result, nil
		}
	}
	return result, nil
}

func (a *App) forgetFinalizedWorktreeProject(request worktree.CleanupRequest) error {
	if err := removeWorktreeProject(request.WorktreeRoot); err != nil {
		return fmt.Errorf("recovery worktree was retained, but the former project registration could not be removed: %w", err)
	}
	forgetWorkspace(request.WorktreeRoot)
	a.catalogRegisteredProjectRoots.Delete(projectRootKey(normalizeProjectRoot(request.WorktreeRoot)))
	if sameProjectRoot(loadWorkspace(), request.WorktreeRoot) {
		saveWorkspace(request.SourceRoot)
	}
	if a.workspaceHub != nil {
		a.workspaceHub.reconcileRoots()
	}
	a.emitProjectTreeChanged()
	return nil
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
	if err := a.requireNavigationIntent(request.NavigationIntentToken); err != nil {
		return CloseMergedWorktreeTabResult{}, err
	}
	releaseRuntime := a.lockRuntimeMutation("close-merged-worktree-tab-snapshot")
	a.sessionRemovalMu.Lock()
	a.mu.Lock()
	tab, err := a.validateMergedWorktreeCloseLocked(request, worktreeKey, sourceKey)
	if err != nil {
		a.mu.Unlock()
		a.sessionRemovalMu.Unlock()
		releaseRuntime()
		return CloseMergedWorktreeTabResult{}, err
	}
	a.mu.Unlock()
	if tab != nil {
		if err := a.snapshotMergedWorktreeCloseTab(tab); err != nil {
			a.sessionRemovalMu.Unlock()
			releaseRuntime()
			return CloseMergedWorktreeTabResult{}, err
		}
	}
	a.sessionRemovalMu.Unlock()
	releaseRuntime()
	if hook := a.navigationIntent.beforeCloseFinalHook; hook != nil {
		hook()
	}

	// Linearization order: navigation fence -> runtime barrier -> removal gate
	// -> App.mu. A newer intent published during the first snapshot wins here.
	a.navigationIntent.mu.Lock()
	defer a.navigationIntent.mu.Unlock()
	if a.navigationIntent.token != strings.TrimSpace(request.NavigationIntentToken) {
		return CloseMergedWorktreeTabResult{}, fmt.Errorf("navigation changed before worktree close; resources were preserved")
	}
	releaseRuntime = a.lockRuntimeMutation("close-merged-worktree-tab-final")
	defer releaseRuntime()
	a.sessionRemovalMu.Lock()
	defer a.sessionRemovalMu.Unlock()
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
	if current == nil {
		a.mu.Unlock()
		return CloseMergedWorktreeTabResult{Closed: true, Idempotent: true}, nil
	}
	a.mu.Unlock()
	if err := a.snapshotMergedWorktreeCloseTab(current); err != nil {
		return CloseMergedWorktreeTabResult{}, err
	}
	a.mu.Lock()
	final, err := a.validateMergedWorktreeCloseLocked(request, worktreeKey, sourceKey)
	if err != nil {
		a.mu.Unlock()
		return CloseMergedWorktreeTabResult{}, err
	}
	if final != current {
		a.mu.Unlock()
		return CloseMergedWorktreeTabResult{}, fmt.Errorf("worktree tab changed at close linearization; resources were preserved")
	}
	a.markTabRemovedLocked(current)
	delete(a.tabs, current.ID)
	a.removeTabOrderLocked(current.ID)
	a.saveTabsLocked()
	a.mu.Unlock()

	if a.terminals != nil {
		a.terminals.closeForTab(current.ID)
	}
	a.closeTabRuntimeAdmissionHeld(current)
	if a.workspaceHub != nil {
		a.workspaceHub.reconcileRoots()
	}
	a.emitProjectTreeRuntimeChangedWithLegacy()
	return CloseMergedWorktreeTabResult{Closed: true}, nil
}

func (a *App) snapshotMergedWorktreeCloseTab(tab *WorkspaceTab) error {
	if err := a.snapshotTab(tab); err != nil {
		return fmt.Errorf("save worktree session before closing: %w", err)
	}
	if err := a.saveTabSessionMetaForCurrentSession(tab); err != nil {
		return fmt.Errorf("save worktree session metadata before closing: %w", err)
	}
	return nil
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

func (a *App) inspectWorktreeMergeRuntimeBlockers(sourceRoot, worktreeRoot string) []worktree.MergeBlocker {
	rootKeys, err := canonicalMergeRuntimeRoots(sourceRoot, worktreeRoot)
	if err != nil {
		return []worktree.MergeBlocker{{Code: "identity", Message: err.Error(), Paths: []string{}}}
	}
	return a.worktreeMergeRuntimeBlockers(rootKeys)
}

func canonicalMergeRuntimeRoots(roots ...string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		key, err := canonicalRuntimeRootErr(root)
		if err != nil {
			return nil, fmt.Errorf("resolve merge runtime identity: %w", err)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("merge runtime identity is empty")
	}
	sort.Strings(out)
	return out, nil
}

func (a *App) worktreeMergeRuntimeBlockers(rootKeys []string) []worktree.MergeBlocker {
	building, active := false, false
	tabIDs := map[string]struct{}{}
	a.mu.RLock()
	for _, tab := range a.runtimeTabsLocked() {
		if tab == nil || !canonicalRootOverlapsAny(canonicalRuntimeRoot(tab.WorkspaceRoot), rootKeys) {
			continue
		}
		tabIDs[tab.ID] = struct{}{}
		if !tab.Ready || tab.Ctrl == nil || strings.TrimSpace(tab.StartupErr) != "" {
			building = true
		}
		if tab.hasActiveRuntimeWork() || mergeActivityActive(tab.ActivityStatus) {
			active = true
		}
	}
	a.mu.RUnlock()

	blockers := []worktree.MergeBlocker{}
	if building {
		blockers = append(blockers, worktree.MergeBlocker{Code: "tab_building", Message: "a source or worktree runtime is still building or unavailable", Paths: []string{}})
	}
	if active {
		blockers = append(blockers, worktree.MergeBlocker{Code: "active_work", Message: "a source or worktree runtime still has active or waiting work", Paths: []string{}})
	}
	if a.terminals != nil && a.terminals.hasRunningForTabs(tabIDs) {
		blockers = append(blockers, worktree.MergeBlocker{Code: "active_terminal", Message: "close source and worktree terminals before merging", Paths: []string{}})
	}
	return blockers
}

func canonicalRootOverlapsAny(candidate string, roots []string) bool {
	for _, root := range roots {
		if pathWithinCanonicalWorktree(candidate, root) || pathWithinCanonicalWorktree(root, candidate) {
			return true
		}
	}
	return false
}

// worktreeMergeReservationSnapshot is immutable after publication. Controller
// publication reads it while holding runtimeAdmissionMu's read side, which is
// ordered after the write-side reservation publication without reacquiring the
// runtime-owner mutex.
type worktreeMergeReservationSnapshot struct {
	roots []string
}

type worktreeRuntimeReservations struct {
	mu            sync.Mutex
	cleanup       map[string]struct{}
	merge         map[string]struct{}
	mergeSnapshot atomic.Pointer[worktreeMergeReservationSnapshot]
}

func (a *App) publishWorktreeMergeReservationSnapshotLocked() {
	roots := make([]string, 0, len(a.worktreeReservations.merge))
	for root := range a.worktreeReservations.merge {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	a.worktreeReservations.mergeSnapshot.Store(&worktreeMergeReservationSnapshot{roots: roots})
}

func (a *App) workspaceMergeReservedSnapshot(workspaceKey string) bool {
	snapshot := a.worktreeReservations.mergeSnapshot.Load()
	return snapshot != nil && canonicalRootOverlapsAny(workspaceKey, snapshot.roots)
}

func mergeRuntimeBlockersError(blockers []worktree.MergeBlocker) error {
	messages := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		messages = append(messages, blocker.Message)
	}
	return fmt.Errorf("merge runtime admission blocked: %s", strings.Join(messages, "; "))
}

// reserveWorktreeMergeRuntime briefly quiesces turn starts and controller
// publication, proves both workspaces are idle, then publishes canonical
// per-root reservations. The global admission barrier is released before Git
// work begins so unrelated workspaces are not frozen for the merge duration.
func (a *App) reserveWorktreeMergeRuntime(sourceRoot, worktreeRoot string) (func(), error) {
	rootKeys, err := canonicalMergeRuntimeRoots(sourceRoot, worktreeRoot)
	if err != nil {
		return nil, err
	}
	a.runtimeAdmissionMu.Lock()
	defer a.runtimeAdmissionMu.Unlock()
	a.worktreeReservations.mu.Lock()
	defer a.worktreeReservations.mu.Unlock()
	if a.worktreeReservations.merge == nil {
		a.worktreeReservations.merge = map[string]struct{}{}
	}
	for _, key := range rootKeys {
		if a.cleanupReservationOverlapsLocked(key) || a.mergeReservationOverlapsLocked(key) {
			return nil, fmt.Errorf("workspace maintenance is already in progress")
		}
	}
	if blockers := a.worktreeMergeRuntimeBlockers(rootKeys); len(blockers) > 0 {
		return nil, mergeRuntimeBlockersError(blockers)
	}
	for _, key := range rootKeys {
		a.worktreeReservations.merge[key] = struct{}{}
	}
	a.publishWorktreeMergeReservationSnapshotLocked()
	return func() {
		a.worktreeReservations.mu.Lock()
		for _, key := range rootKeys {
			delete(a.worktreeReservations.merge, key)
		}
		a.publishWorktreeMergeReservationSnapshotLocked()
		a.worktreeReservations.mu.Unlock()
	}, nil
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
	for _, release := range slices.Backward(releases) {
		release()
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
	for _, component := range slices.Backward(suffix) {
		probe = filepath.Join(probe, component)
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
	for reservedRoot := range a.worktreeReservations.cleanup {
		if pathWithinCanonicalWorktree(workspaceKey, reservedRoot) {
			return true
		}
	}
	return false
}

func (a *App) workspaceMergeReservedLocked(workspaceKey string) bool {
	for reservedRoot := range a.worktreeReservations.merge {
		if pathWithinCanonicalWorktree(workspaceKey, reservedRoot) || pathWithinCanonicalWorktree(reservedRoot, workspaceKey) {
			return true
		}
	}
	return false
}

func (a *App) cleanupReservationOverlapsLocked(worktreeKey string) bool {
	for reservedRoot := range a.worktreeReservations.cleanup {
		if pathWithinCanonicalWorktree(worktreeKey, reservedRoot) || pathWithinCanonicalWorktree(reservedRoot, worktreeKey) {
			return true
		}
	}
	return false
}

func (a *App) mergeReservationOverlapsLocked(workspaceKey string) bool {
	for reservedRoot := range a.worktreeReservations.merge {
		if pathWithinCanonicalWorktree(workspaceKey, reservedRoot) || pathWithinCanonicalWorktree(reservedRoot, workspaceKey) {
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
	// Reserve the complete allocation while the checkout moves to quarantine,
	// so late runtimes cannot enter either path. Adjacent allocations remain
	// independent reservation domains.
	allocationKey, err := canonicalRuntimeRootErr(filepath.Dir(key))
	if err != nil {
		return nil, fmt.Errorf("resolve cleanup allocation identity: %w", err)
	}
	a.worktreeReservations.mu.Lock()
	if a.worktreeReservations.cleanup == nil {
		a.worktreeReservations.cleanup = map[string]struct{}{}
	}
	if a.cleanupReservationOverlapsLocked(allocationKey) || a.mergeReservationOverlapsLocked(allocationKey) {
		a.worktreeReservations.mu.Unlock()
		return nil, fmt.Errorf("worktree maintenance is already in progress")
	}
	a.mu.RLock()
	referenced := a.runtimeReferencesCanonicalLocked(allocationKey)
	if !referenced {
		a.worktreeReservations.cleanup[allocationKey] = struct{}{}
	}
	a.mu.RUnlock()
	a.worktreeReservations.mu.Unlock()
	if referenced {
		return nil, fmt.Errorf("a visible or background runtime still references the worktree; it was preserved")
	}
	return func() {
		a.worktreeReservations.mu.Lock()
		delete(a.worktreeReservations.cleanup, allocationKey)
		a.worktreeReservations.mu.Unlock()
	}, nil
}

// beginWorkspaceRuntimeAdmission holds every worktree maintenance-reservation
// gate through a runtime owner's final App.mu publication. Callers must invoke
// it before acquiring App.mu and defer the returned release.
func (a *App) beginWorkspaceRuntimeAdmission(workspaceRoot string) (func(), error) {
	key, err := canonicalRuntimeRootErr(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime workspace identity: %w", err)
	}
	a.worktreeReservations.mu.Lock()
	if err := a.workspaceRuntimeReservationErrLocked(key); err != nil {
		a.worktreeReservations.mu.Unlock()
		return nil, fmt.Errorf("%w; retry after maintenance completes", err)
	}
	return a.worktreeReservations.mu.Unlock, nil
}
