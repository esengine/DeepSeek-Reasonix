package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/taskcatalog"
	"reasonix/internal/taskmonitor"
)

type taskKillController struct {
	control.SessionAPI
	mu     sync.Mutex
	killed []string
}

func (c *taskKillController) CancelJob(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killed = append(c.killed, id)
	return true
}

func (c *taskKillController) killCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.killed)
}

func (c *taskKillController) killedIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.killed...)
}

func TestTaskControlConcurrentInitializationReturnsOneService(t *testing.T) {
	app := &App{}
	const callers = 16
	services := make(chan *taskmonitor.ControlService, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			services <- app.taskControl()
		})
	}
	wg.Wait()
	close(services)

	var first *taskmonitor.ControlService
	for service := range services {
		if first == nil {
			first = service
			continue
		}
		if service != first {
			t.Fatal("taskControl returned more than one process-wide service")
		}
	}
}

func TestDesktopTaskJobKillerRoutesBySessionNotActiveTab(t *testing.T) {
	projectA := t.TempDir()
	projectB := t.TempDir()
	pathA := filepath.Join(t.TempDir(), "shared-session.jsonl")
	pathB := filepath.Join(t.TempDir(), "shared-session.jsonl")
	ctrlA := &taskKillController{SessionAPI: control.New(control.Options{Label: "a", SessionPath: pathA})}
	ctrlB := &taskKillController{SessionAPI: control.New(control.Options{Label: "b", SessionPath: pathB})}
	defer ctrlA.Close()
	defer ctrlB.Close()

	app := &App{
		tabs: map[string]*WorkspaceTab{
			"active-a": {ID: "active-a", WorkspaceRoot: projectA, Ctrl: ctrlA},
		},
		detachedSessions: map[string]*WorkspaceTab{
			sessionRuntimeKey(pathB): {ID: "detached-b", WorkspaceRoot: projectB, Ctrl: ctrlB},
		},
		activeTabID: "active-a",
	}

	if agent.BranchID(pathA) != agent.BranchID(pathB) {
		t.Fatal("test setup must use colliding session IDs")
	}
	killer := desktopTaskJobKiller{app: app, projectDir: projectB}
	if !killer.Kill(agent.BranchID(pathB), "task-1") {
		t.Fatal("expected detached project B task to be killed")
	}
	if ctrlA.killCount() != 0 || ctrlB.killCount() != 1 {
		t.Fatalf("kill routed incorrectly: active=%d detached=%d", ctrlA.killCount(), ctrlB.killCount())
	}
}

func TestDesktopTaskJobKillerRefusesLegacyTaskWithoutSession(t *testing.T) {
	root := t.TempDir()
	path := agent.NewSessionPath(t.TempDir(), "session")
	ctrl := &taskKillController{SessionAPI: control.New(control.Options{Label: "session", SessionPath: path})}
	defer ctrl.Close()
	app := &App{tabs: map[string]*WorkspaceTab{"active": {ID: "active", WorkspaceRoot: root, Ctrl: ctrl}}}

	if (desktopTaskJobKiller{app: app, projectDir: root}).Kill("", "task-1") {
		t.Fatal("legacy task without session ID must not be routed by colliding task ID")
	}
	if ctrl.killCount() != 0 {
		t.Fatalf("legacy task unexpectedly killed %d runtime(s)", ctrl.killCount())
	}
}

func TestTaskMonitorUsesActiveWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"active": {ID: "active", Scope: "project", WorkspaceRoot: root},
		},
		activeTabID: "active",
	}
	if got := app.projectDir(); got != root {
		t.Fatalf("projectDir = %q, want active workspace %q", got, root)
	}
}

func TestTaskActionProjectResolvesAllowlistWithoutCatalog(t *testing.T) {
	root := t.TempDir()
	app := &App{
		ctx: context.Background(),
		tabs: map[string]*WorkspaceTab{
			"active": {ID: "active", Scope: "project", WorkspaceRoot: root},
		},
		activeTabID: "active",
	}
	key := taskcatalog.ProjectKey(root)
	project, err := app.taskActionProject(key)
	if err != nil {
		t.Fatalf("taskActionProject without catalog: %v", err)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if project.Root != root || project.Key != key {
		t.Fatalf("project=%#v, want root=%q key=%q", project, root, key)
	}
	if _, err := app.taskActionProject("not-a-real-project-key"); err == nil {
		t.Fatal("expected unknown project key error")
	}
}

func TestStopTaskRoutesMonitorIdentityToRuntimeJob(t *testing.T) {
	root := t.TempDir()
	path := agent.NewSessionPath(t.TempDir(), "session")
	ctrl := &taskKillController{SessionAPI: control.New(control.Options{Label: "session", SessionPath: path})}
	defer ctrl.Close()
	app := &App{
		ctx: context.Background(),
		tabs: map[string]*WorkspaceTab{
			"active": {ID: "active", Scope: "project", WorkspaceRoot: root, Ctrl: ctrl},
		},
		activeTabID: "active",
	}
	sessionID := agent.BranchID(path)
	monitorID := sessionID + "--task-1"
	now := time.Now()
	if err := app.taskStore().SaveTask(app.ctx, root, taskmonitor.TaskSnapshot{
		SchemaVersion: 1, TaskID: monitorID, JobID: "task-1", SessionID: sessionID,
		State: taskmonitor.TaskStateRunning, RuntimeState: taskmonitor.RuntimeStateAlive,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := app.StopTask(monitorID, 1, "", "desktop-route")
	if err != nil || !res.Accepted {
		t.Fatalf("StopTask: result=%+v err=%v", res, err)
	}
	ids := ctrl.killedIDs()
	if len(ids) != 1 || ids[0] != "task-1" {
		t.Fatalf("runtime killed IDs = %v, want [task-1]", ids)
	}
}

func TestStopTaskForTabKeepsSourceWorkspaceAfterActiveTabSwitch(t *testing.T) {
	projectA := t.TempDir()
	projectB := t.TempDir()
	pathA := filepath.Join(t.TempDir(), "shared-session.jsonl")
	pathB := filepath.Join(t.TempDir(), "shared-session.jsonl")
	ctrlA := &taskKillController{SessionAPI: control.New(control.Options{Label: "a", SessionPath: pathA})}
	ctrlB := &taskKillController{SessionAPI: control.New(control.Options{Label: "b", SessionPath: pathB})}
	defer ctrlA.Close()
	defer ctrlB.Close()

	app := &App{
		ctx: context.Background(),
		tabs: map[string]*WorkspaceTab{
			"tab-a": {ID: "tab-a", Scope: "project", WorkspaceRoot: projectA, SessionPath: pathA, Ctrl: ctrlA},
			"tab-b": {ID: "tab-b", Scope: "project", WorkspaceRoot: projectB, SessionPath: pathB, Ctrl: ctrlB},
		},
		activeTabID: "tab-b",
	}
	sessionID := agent.BranchID(pathA)
	if sessionID != agent.BranchID(pathB) {
		t.Fatal("test setup must use colliding session IDs")
	}
	monitorID := sessionID + "--task-1"
	now := time.Now()
	for _, root := range []string{projectA, projectB} {
		if err := app.taskStore().SaveTask(app.ctx, root, taskmonitor.TaskSnapshot{
			SchemaVersion: 1, TaskID: monitorID, JobID: "task-1", SessionID: sessionID,
			State: taskmonitor.TaskStateRunning, RuntimeState: taskmonitor.RuntimeStateAlive,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	res, err := app.StopTaskForTab("tab-a", monitorID, 1, "", "tab-bound-stop")
	if err != nil || !res.Accepted {
		t.Fatalf("StopTaskForTab: result=%+v err=%v", res, err)
	}
	if ctrlA.killCount() != 1 || ctrlB.killCount() != 0 {
		t.Fatalf("kill routed away from source tab: projectA=%d projectB=%d", ctrlA.killCount(), ctrlB.killCount())
	}
	projectBTask, err := app.taskStore().GetTask(app.ctx, projectB, monitorID)
	if err != nil || projectBTask == nil || projectBTask.State != taskmonitor.TaskStateRunning {
		t.Fatalf("project B task mutated by project A control: task=%+v err=%v", projectBTask, err)
	}
}

func TestListSessionsForTabKeepsSourceDirectoryAfterActiveTabSwitch(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	pathA := filepath.Join(dirA, "a.jsonl")
	pathB := filepath.Join(dirB, "b.jsonl")
	if err := os.WriteFile(pathA, []byte(`{"role":"user","content":"workspace A"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte(`{"role":"user","content":"workspace B"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctrlA := control.New(control.Options{SessionDir: dirA, SessionPath: pathA, Label: "a"})
	ctrlB := control.New(control.Options{SessionDir: dirB, SessionPath: pathB, Label: "b"})
	defer ctrlA.Close()
	defer ctrlB.Close()
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"tab-a": {ID: "tab-a", Scope: "project", WorkspaceRoot: t.TempDir(), SessionPath: pathA, Ctrl: ctrlA},
			"tab-b": {ID: "tab-b", Scope: "project", WorkspaceRoot: t.TempDir(), SessionPath: pathB, Ctrl: ctrlB},
		},
		activeTabID: "tab-b",
	}
	installSessionCatalogForTest(t, app, dirA, "global", "")

	sessions := app.ListSessionsForTab("tab-a")
	if len(sessions) != 1 || sessions[0].Path != pathA || sessions[0].TurnsState != "unknown" {
		t.Fatalf("ListSessionsForTab(tab-a) = %+v", sessions)
	}
}
