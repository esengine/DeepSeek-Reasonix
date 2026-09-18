package main

import (
	"testing"
	"time"
)

func TestRemoveWorkspaceDeduplicatesConcurrentCalls(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", Scope: "project", WorkspaceRoot: projectRoot, Ready: true, disabledMCP: map[string]ServerView{}},
			"global":  {ID: "global", Scope: "global", WorkspaceRoot: globalTabWorkspaceRoot(), Ready: true, disabledMCP: map[string]ServerView{}},
		},
		tabOrder:         []string{"project", "global"},
		activeTabID:      "project",
		detachedSessions: map[string]*WorkspaceTab{},
	}
	runtimeEntries := make(chan struct{}, 2)
	release := make(chan struct{})
	joined := make(chan struct{}, 1)
	app.runtimeMutationBeforeLockHook = func(operation string) {
		if operation != "remove-workspace" {
			return
		}
		runtimeEntries <- struct{}{}
		<-release
	}
	app.workspaceRemovalFlightJoinHook = func(dir string) {
		if dir != normalizeProjectRoot(projectRoot) {
			t.Errorf("joined workspace = %q, want %q", dir, normalizeProjectRoot(projectRoot))
		}
		joined <- struct{}{}
	}

	errs := make(chan error, 2)
	go func() { errs <- app.RemoveWorkspace(projectRoot) }()
	select {
	case <-runtimeEntries:
	case <-time.After(time.Second):
		t.Fatal("first removal did not reach runtime mutation boundary")
	}
	go func() { errs <- app.RemoveWorkspace(projectRoot) }()
	select {
	case <-joined:
	case <-runtimeEntries:
		t.Fatal("concurrent removal started a second workspace transaction")
	case <-time.After(time.Second):
		t.Fatal("concurrent removal did not join the published flight")
	}
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("RemoveWorkspace: %v", err)
		}
	}
	select {
	case <-runtimeEntries:
		t.Fatal("workspace transaction ran more than once")
	default:
	}
}

func TestRemoveWorkspaceShutdownPreventsFallbackRuntimeStart(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", Scope: "project", WorkspaceRoot: projectRoot, Ready: true, disabledMCP: map[string]ServerView{}},
		},
		tabOrder:         []string{"project"},
		activeTabID:      "project",
		detachedSessions: map[string]*WorkspaceTab{},
	}
	app.lifecycleCheckpointHook = func(phase string) {
		if phase == "remove-workspace-before-fallback" {
			app.shuttingDown.Store(true)
		}
	}

	if err := app.RemoveWorkspace(projectRoot); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	app.mu.RLock()
	defer app.mu.RUnlock()
	if len(app.tabs) != 1 {
		t.Fatalf("tabs = %d, want one inert fallback tab", len(app.tabs))
	}
	for _, tab := range app.tabs {
		if tab.Scope != "global" || tab.Ctrl != nil || tab.Ready || tab.buildGeneration != 0 || tab.buildDone != nil {
			t.Fatalf("fallback runtime started during shutdown: %+v", tab)
		}
	}
}
