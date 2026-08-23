package main

import (
	"fmt"
	"strings"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/workspacelease"
)

// ActiveWorkView is the structured Desktop contract for work that prevents a
// controller rebuild or a destructive tab action. Jobs is always a JSON array.
type ActiveWorkView struct {
	Running       bool      `json:"running"`
	PendingPrompt bool      `json:"pendingPrompt"`
	Cancellable   bool      `json:"cancellable"`
	Jobs          []JobView `json:"jobs"`
}

// JobCancelBatchView reports which requested jobs accepted cancellation. Both
// slices are initialized so Wails never sends null to React.
type JobCancelBatchView struct {
	Cancelled  []string `json:"cancelled"`
	NotRunning []string `json:"notRunning"`
}

// BackgroundRuntimeView is one visible or detached runtime with active work.
// TabID is an opaque process-local handle; paths and session writer ids are
// deliberately omitted from this user-facing contract.
type BackgroundRuntimeView struct {
	TabID         string    `json:"tabId"`
	Title         string    `json:"title"`
	Detached      bool      `json:"detached"`
	Running       bool      `json:"running"`
	PendingPrompt bool      `json:"pendingPrompt"`
	Jobs          []JobView `json:"jobs"`
}

// WorkspaceConflictView describes a currently-waiting Delivery writer without
// exposing the lock path, process id, or session path.
type WorkspaceConflictView struct {
	State             string         `json:"state"`
	OwnerTabID        string         `json:"ownerTabId,omitempty"`
	OwnerTitle        string         `json:"ownerTitle,omitempty"`
	OwnerScope        string         `json:"ownerScope,omitempty"`
	OwnerLabel        string         `json:"ownerLabel,omitempty"`
	OwnerWork         ActiveWorkView `json:"ownerWork"`
	CanReveal         bool           `json:"canReveal"`
	CanCreateWorktree bool           `json:"canCreateWorktree"`
}

func activeWorkForController(ctrl control.SessionAPI) ActiveWorkView {
	view := ActiveWorkView{Jobs: []JobView{}}
	if ctrl == nil {
		return view
	}
	status := ctrl.RuntimeStatus()
	view.Running = status.Running
	view.PendingPrompt = status.PendingPrompt
	view.Cancellable = status.Cancellable
	for _, job := range ctrl.Jobs() {
		view.Jobs = append(view.Jobs, JobView{
			ID: job.ID, Kind: job.Kind, Label: job.Label,
			Status: job.Status, StartedAt: job.StartedAt,
		})
	}
	return view
}

func (v ActiveWorkView) active() bool {
	return v.Running || v.PendingPrompt || len(v.Jobs) > 0
}

// ActiveWorkForTab returns the precise blocker state for one tab. It is a
// preflight aid only; rebuild paths still re-check active work atomically.
func (a *App) ActiveWorkForTab(tabID string) ActiveWorkView {
	return activeWorkForController(a.ctrlForRuntimeTabID(tabID))
}

// CancelJobsForTab requests cancellation for a stable tab id. The job manager
// keeps cancelled-but-unwinding jobs visible until their done channels close.
func (a *App) CancelJobsForTab(tabID string, jobIDs []string) (JobCancelBatchView, error) {
	result := JobCancelBatchView{Cancelled: []string{}, NotRunning: []string{}}
	seen := map[string]bool{}
	for _, raw := range jobIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cancelled, err := a.CancelJobForTab(tabID, id)
		if err != nil {
			return result, err
		}
		if cancelled {
			result.Cancelled = append(result.Cancelled, id)
		} else {
			result.NotRunning = append(result.NotRunning, id)
		}
	}
	return result, nil
}

type backgroundRuntimeSnapshot struct {
	id       string
	ctrl     control.SessionAPI
	detached bool
	title    string
}

// BackgroundRuntimes returns every process-local runtime that still needs a
// visible recovery path, including runtimes detached by an explicit tab close.
func (a *App) BackgroundRuntimes() []BackgroundRuntimeView {
	a.mu.RLock()
	snapshots := make([]backgroundRuntimeSnapshot, 0, len(a.tabs)+len(a.detachedSessions))
	seen := map[*WorkspaceTab]bool{}
	for _, tab := range a.tabs {
		if tab == nil || seen[tab] {
			continue
		}
		seen[tab] = true
		title := strings.TrimSpace(tab.TopicTitle)
		if title == "" {
			title = strings.TrimSpace(tab.Label)
		}
		snapshots = append(snapshots, backgroundRuntimeSnapshot{id: tab.ID, ctrl: tab.Ctrl, title: title})
	}
	for _, tab := range a.detachedSessions {
		if tab == nil || seen[tab] {
			continue
		}
		seen[tab] = true
		title := strings.TrimSpace(tab.TopicTitle)
		if title == "" {
			title = strings.TrimSpace(tab.Label)
		}
		snapshots = append(snapshots, backgroundRuntimeSnapshot{id: tab.ID, ctrl: tab.Ctrl, detached: true, title: title})
	}
	a.mu.RUnlock()

	out := make([]BackgroundRuntimeView, 0, len(snapshots))
	for _, snapshot := range snapshots {
		work := activeWorkForController(snapshot.ctrl)
		if !work.active() {
			continue
		}
		out = append(out, BackgroundRuntimeView{
			TabID: snapshot.id, Title: snapshot.title, Detached: snapshot.detached,
			Running: work.Running, PendingPrompt: work.PendingPrompt, Jobs: work.Jobs,
		})
	}
	return out
}

func (a *App) ctrlForRuntimeTabID(tabID string) control.SessionAPI {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if strings.TrimSpace(tabID) == "" {
		return a.activeCtrlLocked()
	}
	tab := a.tabByEventSinkIDLocked(tabID)
	if tab == nil {
		return nil
	}
	return tab.Ctrl
}

// RevealBackgroundRuntime activates a visible owner or reopens the exact
// detached session. It never reattaches by workspace alone.
func (a *App) RevealBackgroundRuntime(tabID string) (TabMeta, error) {
	a.mu.RLock()
	if tab := a.tabs[tabID]; tab != nil {
		a.mu.RUnlock()
		if err := a.SetActiveTab(tabID); err != nil {
			return TabMeta{}, err
		}
		a.mu.RLock()
		current := a.tabs[tabID]
		if current == nil {
			a.mu.RUnlock()
			return TabMeta{}, fmt.Errorf("background task is no longer available")
		}
		meta := a.tabMeta(current, true)
		a.mu.RUnlock()
		return enrichTabMeta(meta), nil
	}
	tab := a.tabByEventSinkIDLocked(tabID)
	if tab == nil || tab.Ctrl == nil {
		a.mu.RUnlock()
		return TabMeta{}, fmt.Errorf("background task is no longer available")
	}
	scope := tab.Scope
	workspaceRoot := tab.WorkspaceRoot
	topicID := tab.TopicID
	sessionPath := tab.currentSessionPath()
	a.mu.RUnlock()
	if strings.TrimSpace(sessionPath) == "" {
		return TabMeta{}, fmt.Errorf("background task session is unavailable")
	}
	return a.OpenTopicSession(scope, workspaceRoot, topicID, sessionPath)
}

type workspaceLeaseReporter interface {
	WorkspaceLeaseState() workspacelease.State
	WorkspaceLeaseHeldKeys() []string
}

func controllerWorkspaceLeaseState(ctrl control.SessionAPI) workspacelease.State {
	if reporter, ok := ctrl.(workspaceLeaseReporter); ok {
		return reporter.WorkspaceLeaseState()
	}
	return workspacelease.State{}
}

func leaseDomainsOverlap(waitingRoot string, waiting workspacelease.State, holderRoot string, holder workspacelease.State) bool {
	return workspacelease.LeaseStatesOverlap(waitingRoot, waiting, holderRoot, holder)
}

// WorkspaceConflictForTab classifies the owner that a Delivery writer is
// currently waiting for. An acquired process-local owner is actionable; when no
// local owner matches, the OS lock is treated as external.
func (a *App) WorkspaceConflictForTab(tabID string) WorkspaceConflictView {
	empty := WorkspaceConflictView{State: "none", OwnerWork: ActiveWorkView{Jobs: []JobView{}}}
	a.mu.RLock()
	var target *WorkspaceTab
	if strings.TrimSpace(tabID) == "" {
		target = a.activeTabLocked()
	} else {
		target = a.tabByEventSinkIDLocked(tabID)
	}
	if target == nil {
		a.mu.RUnlock()
		return empty
	}
	targetCtrl := target.Ctrl
	targetWorkspaceRoot := target.WorkspaceRoot
	a.mu.RUnlock()
	if targetCtrl == nil {
		return empty
	}
	targetState := controllerWorkspaceLeaseState(targetCtrl)
	if !targetState.Waiting {
		return empty
	}
	targetRoot, err := workspacelease.CanonicalWorkspace(targetWorkspaceRoot)
	if err != nil {
		return empty
	}
	availability := a.DeliveryWorktreeAvailability(targetWorkspaceRoot)

	a.mu.RLock()
	type candidate struct {
		id    string
		ctrl  control.SessionAPI
		root  string
		title string
	}
	candidates := make([]candidate, 0, len(a.tabs)+len(a.detachedSessions))
	seen := map[*WorkspaceTab]bool{}
	for _, tab := range a.runtimeTabsLocked() {
		if tab == nil || tab == target || seen[tab] || tab.Ctrl == nil {
			continue
		}
		seen[tab] = true
		title := strings.TrimSpace(tab.TopicTitle)
		if title == "" {
			title = strings.TrimSpace(tab.Label)
		}
		candidates = append(candidates, candidate{id: tab.ID, ctrl: tab.Ctrl, root: tab.WorkspaceRoot, title: title})
	}
	a.mu.RUnlock()

	for _, candidate := range candidates {
		root, err := workspacelease.CanonicalWorkspace(candidate.root)
		ownerState := controllerWorkspaceLeaseState(candidate.ctrl)
		if err != nil || !ownerState.Acquired {
			continue
		}
		if !leaseDomainsOverlap(targetRoot, targetState, root, ownerState) {
			continue
		}
		ownerScope, ownerLabel := ownerState.HeldScope, ownerState.HeldLabel
		if ownerScope == "" {
			ownerScope, ownerLabel = ownerState.Scope, ownerState.Label
		}
		return WorkspaceConflictView{
			State: "local", OwnerTabID: candidate.id, OwnerTitle: candidate.title,
			OwnerScope: ownerScope, OwnerLabel: ownerLabel,
			OwnerWork: activeWorkForController(candidate.ctrl), CanReveal: true,
			CanCreateWorktree: availability.Available,
		}
	}
	empty.State = "external"
	empty.CanCreateWorktree = availability.Available
	return empty
}

// RevealWorkspaceWriterForTab opens the exact process-local runtime identified
// by WorkspaceConflictForTab. External writers remain non-actionable.
func (a *App) RevealWorkspaceWriterForTab(tabID string) (TabMeta, error) {
	conflict := a.WorkspaceConflictForTab(tabID)
	if conflict.State != "local" || conflict.OwnerTabID == "" {
		return TabMeta{}, fmt.Errorf("the workspace writer is not available in this Reasonix window")
	}
	return a.RevealBackgroundRuntime(conflict.OwnerTabID)
}

const stopAndCloseGrace = 15 * time.Second

// CloseTabWithPolicy makes the old implicit detach behavior an explicit user
// choice. stop_and_close never removes the tab until all owned work is idle.
func (a *App) CloseTabWithPolicy(tabID, policy string) error {
	switch strings.TrimSpace(policy) {
	case "keep_running":
		return a.closeTab(tabID, true)
	case "stop_and_close":
		ctrl := a.ctrlForRuntimeTabID(tabID)
		if ctrl == nil {
			return a.closeTab(tabID, false)
		}
		ctrl.Cancel()
		jobs := ctrl.Jobs()
		ids := make([]string, 0, len(jobs))
		for _, job := range jobs {
			ids = append(ids, job.ID)
		}
		if _, err := a.CancelJobsForTab(tabID, ids); err != nil {
			return err
		}
		deadline := time.NewTimer(stopAndCloseGrace)
		defer deadline.Stop()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			if !activeWorkForController(ctrl).active() {
				return a.closeTab(tabID, false)
			}
			select {
			case <-deadline.C:
				return fmt.Errorf("background work did not stop within %s; the task was kept open", stopAndCloseGrace)
			case <-ticker.C:
			}
		}
	default:
		return fmt.Errorf("unknown close policy %q", policy)
	}
}
