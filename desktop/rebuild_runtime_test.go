package main

import (
	"sync"
	"testing"

	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/extension"
)

func TestRuntimeDoctorEmptyApp(t *testing.T) {
	var a *App
	report := a.RuntimeDoctor()
	if report.Text == "" {
		t.Fatal("expected doctor text")
	}
	// Nil app still returns process-wide metrics/recoverability.
	if !report.AllowResume {
		t.Fatal("empty process should allow resume")
	}
}

func TestRuntimeDoctorConcurrentWithBuildResultUpdate(t *testing.T) {
	ctrl := &control.Controller{}
	tab := &WorkspaceTab{ID: "tab-1", Ctrl: ctrl}
	a := &App{
		tabs:        map[string]*WorkspaceTab{tab.ID: tab},
		activeTabID: tab.ID,
	}
	first := &boot.BuildResult{Controller: ctrl, Owner: extension.NewRuntimeOwner()}
	second := &boot.BuildResult{Controller: ctrl, Owner: extension.NewRuntimeOwner()}
	a.setTabLastBuildResult(tab, first)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		<-start
		for range 100 {
			a.setTabLastBuildResult(tab, second)
			a.setTabLastBuildResult(tab, first)
		}
	})
	close(start)
	for range 100 {
		report := a.RuntimeDoctor()
		if report.Text == "" {
			t.Fatal("expected doctor text")
		}
	}
	wg.Wait()
}

func TestTabBuildResultMustMatchCurrentController(t *testing.T) {
	current := &control.Controller{}
	stale := &control.Controller{}
	tab := &WorkspaceTab{ID: "tab-1", Ctrl: current}
	a := &App{
		tabs:        map[string]*WorkspaceTab{tab.ID: tab},
		activeTabID: tab.ID,
	}
	staleResult := &boot.BuildResult{Controller: stale, Owner: extension.NewRuntimeOwner()}
	a.setTabLastBuildResult(tab, staleResult)
	if got := a.tabBuildResultForController(tab, current); got != nil {
		t.Fatal("stale build result was reused for a replacement controller")
	}

	currentResult := &boot.BuildResult{Controller: current, Owner: extension.NewRuntimeOwner()}
	a.setTabLastBuildResult(tab, currentResult)
	if got := a.tabBuildResultForController(tab, current); got != currentResult {
		t.Fatal("current controller build result was not reused")
	}
}
