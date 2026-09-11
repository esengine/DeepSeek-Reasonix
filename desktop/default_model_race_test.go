package main

import "testing"

func TestSetDefaultModelConcurrentTabModelUpdateIsRaceFree(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, newRef := configureSwitchableDefaultModels(t)

	app := NewApp()
	tab := testTab("default-model-race", globalWorkspaceRoot())
	tab.Scope = "global"
	tab.model = oldRef
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	})

	started := make(chan struct{})
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.mu.Lock()
		tab.model = oldRef
		app.mu.Unlock()
		close(started)
		for {
			select {
			case <-stop:
				return
			default:
				app.mu.Lock()
				tab.model = oldRef
				app.mu.Unlock()
			}
		}
	}()
	<-started

	err := app.SetDefaultModel(newRef)
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("SetDefaultModel: %v", err)
	}
}
