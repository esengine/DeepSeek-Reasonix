package main

import (
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/jobs"
	"reasonix/internal/workspacelease"
)

type backgroundRuntimeController struct {
	stubSessionAPI
	status     control.RuntimeStatus
	jobs       []jobs.View
	lease      workspacelease.State
	cancelled  []string
	turnCancel int
}

func (c *backgroundRuntimeController) RuntimeStatus() control.RuntimeStatus { return c.status }
func (c *backgroundRuntimeController) Jobs() []jobs.View {
	return append([]jobs.View(nil), c.jobs...)
}
func (c *backgroundRuntimeController) CancelJob(id string) bool {
	for _, job := range c.jobs {
		if job.ID == id {
			c.cancelled = append(c.cancelled, id)
			return true
		}
	}
	return false
}
func (c *backgroundRuntimeController) Cancel() { c.turnCancel++ }
func (c *backgroundRuntimeController) WorkspaceLeaseState() workspacelease.State {
	return c.lease
}

func TestBackgroundRuntimeAPIsKeepDetachedJobsActionable(t *testing.T) {
	targetCtrl := &backgroundRuntimeController{
		status: control.RuntimeStatus{BackgroundJobs: 1},
		jobs:   []jobs.View{{ID: "job-target", Kind: "bash", Label: "target tests", Status: "running", StartedAt: 1}},
	}
	focusedCtrl := &backgroundRuntimeController{
		status: control.RuntimeStatus{BackgroundJobs: 1},
		jobs:   []jobs.View{{ID: "job-focused", Kind: "bash", Label: "focused tests", Status: "running", StartedAt: 2}},
	}
	target := &WorkspaceTab{ID: "target", TopicTitle: "Detached task", Ctrl: targetCtrl}
	focused := &WorkspaceTab{ID: "focused", TopicTitle: "Focused task", Ctrl: focusedCtrl}
	app := &App{
		tabs:             map[string]*WorkspaceTab{"focused": focused},
		tabOrder:         []string{"focused"},
		activeTabID:      "focused",
		detachedSessions: map[string]*WorkspaceTab{"session-target": target},
	}

	work := app.ActiveWorkForTab("target")
	if len(work.Jobs) != 1 || work.Jobs[0].ID != "job-target" {
		t.Fatalf("ActiveWorkForTab(target) = %+v, want target job", work)
	}
	result, err := app.CancelJobsForTab("target", []string{"job-target", "job-target", "missing"})
	if err != nil {
		t.Fatalf("CancelJobsForTab: %v", err)
	}
	if len(result.Cancelled) != 1 || result.Cancelled[0] != "job-target" || len(result.NotRunning) != 1 || result.NotRunning[0] != "missing" {
		t.Fatalf("CancelJobsForTab result = %+v", result)
	}
	if len(targetCtrl.cancelled) != 1 || len(focusedCtrl.cancelled) != 0 {
		t.Fatalf("cancellation routing target=%v focused=%v", targetCtrl.cancelled, focusedCtrl.cancelled)
	}

	runtimes := app.BackgroundRuntimes()
	if len(runtimes) != 2 {
		t.Fatalf("BackgroundRuntimes = %+v, want visible and detached", runtimes)
	}
	var foundDetached bool
	for _, runtime := range runtimes {
		if runtime.TabID == "target" {
			foundDetached = runtime.Detached && runtime.Title == "Detached task" && len(runtime.Jobs) == 1
		}
	}
	if !foundDetached {
		t.Fatalf("detached runtime missing from %+v", runtimes)
	}
}

func TestCancelJobForLocalRuntimeByExplicitTab(t *testing.T) {
	localCtrl := &backgroundRuntimeController{
		status: control.RuntimeStatus{BackgroundJobs: 1},
		jobs:   []jobs.View{{ID: "job-local", Kind: "bash", Status: "running"}},
	}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"local": {ID: "local", Ctrl: localCtrl}},
		tabOrder:    []string{"local"},
		activeTabID: "local",
	}

	listed := app.JobsForTab("local")
	if len(listed) != 1 || listed[0].ID != "job-local" {
		t.Fatalf("JobsForTab(local) = %+v, want local job", listed)
	}
	cancelled, err := app.CancelJobForTab("local", "job-local")
	if err != nil {
		t.Fatalf("CancelJobForTab(local): %v", err)
	}
	if !cancelled || len(localCtrl.cancelled) != 1 || localCtrl.cancelled[0] != "job-local" {
		t.Fatalf("local cancellation = %t, calls=%v; want local job cancelled", cancelled, localCtrl.cancelled)
	}
}

func TestWorkspaceConflictIdentifiesExactLocalWriterWithoutIdentity(t *testing.T) {
	root := t.TempDir()
	waiterCtrl := &backgroundRuntimeController{
		status: control.RuntimeStatus{Running: true},
		lease:  workspacelease.State{Waiting: true},
	}
	ownerCtrl := &backgroundRuntimeController{
		status: control.RuntimeStatus{BackgroundJobs: 1},
		jobs:   []jobs.View{{ID: "owner-job", Kind: "bash", Status: "running"}},
		lease:  workspacelease.State{Acquired: true},
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"waiter": {ID: "waiter", WorkspaceRoot: root, Ctrl: waiterCtrl},
			"owner":  {ID: "owner", TopicTitle: "Writing task", WorkspaceRoot: root, Ctrl: ownerCtrl},
		},
		tabOrder:    []string{"waiter", "owner"},
		activeTabID: "waiter",
	}

	conflict := app.WorkspaceConflictForTab("waiter")
	if conflict.State != "local" || conflict.OwnerTabID != "owner" || conflict.OwnerTitle != "Writing task" || !conflict.CanReveal {
		t.Fatalf("WorkspaceConflictForTab = %+v, want exact local owner", conflict)
	}
	if len(conflict.OwnerWork.Jobs) != 1 || conflict.OwnerWork.Jobs[0].ID != "owner-job" {
		t.Fatalf("owner work = %+v, want sanitized active work", conflict.OwnerWork)
	}
}
