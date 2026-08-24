package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// injectHeartbeatStubCtrl waits for the heartbeat execution path to open a tab
// and replaces its in-flight controller build with the stub, mirroring the
// per-test injection loops in the tests above.
func injectHeartbeatStubCtrl(app *App, ctrl *heartbeatExecuteTaskCtrlStub) chan struct{} {
	injected := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-injected:
				return
			case <-ticker.C:
				var cancel context.CancelFunc
				var tabToInject *WorkspaceTab
				app.mu.Lock()
				for _, tab := range app.tabs {
					if tab == nil {
						continue
					}
					tab.removed = true
					cancel = tab.buildCancel
					tabToInject = tab
					break
				}
				app.mu.Unlock()
				if tabToInject == nil {
					continue
				}
				if cancel != nil {
					cancel()
				}
				app.mu.Lock()
				if tabToInject.Ctrl == nil {
					tabToInject.Ctrl = ctrl
					tabToInject.Ready = true
					tabToInject.StartupErr = ""
					app.advanceSessionRuntimeEpochLocked(tabToInject)
					app.mu.Unlock()
					close(injected)
					return
				}
				app.mu.Unlock()
			}
		}
	}()
	return injected
}

func TestHeartbeatExecuteTaskSwitchesToConfiguredModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.runtimeEvents.emit = func(context.Context, string, ...any) {}
	engine := &HeartbeatEngine{
		app:           app,
		pendingTopics: map[string]heartbeatPendingTopic{},
	}
	seed := HeartbeatTask{
		ID:           "model-task",
		Title:        "Model task",
		Prompt:       "ping",
		ApprovalMode: "auto",
		Model:        "deepseek/deepseek-v4",
	}
	if err := engine.saveTasks([]HeartbeatTask{seed}); err != nil {
		t.Fatal(err)
	}
	engine.ReloadConfig()
	var switchedTab, switchedModel string
	engine.applyTaskModel = func(tabID, model string) error {
		switchedTab, switchedModel = tabID, model
		return nil
	}
	ctrl := &heartbeatExecuteTaskCtrlStub{}
	injectHeartbeatStubCtrl(app, ctrl)

	got := engine.executeTask(seed)

	if switchedTab == "" {
		t.Fatal("configured model should switch the task tab before submit")
	}
	if switchedModel != "deepseek/deepseek-v4" {
		t.Fatalf("switched model = %q, want deepseek/deepseek-v4", switchedModel)
	}
	if len(ctrl.submitted) != 1 || ctrl.submitted[0] != "ping" {
		t.Fatalf("submitted prompts = %v, want [ping] after model switch", ctrl.submitted)
	}
	if got.LastRunAt == 0 {
		t.Fatal("model switch success should complete the run")
	}
}

func TestHeartbeatExecuteTaskSkipsOnFailedModelSwitch(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.runtimeEvents.emit = func(context.Context, string, ...any) {}
	engine := &HeartbeatEngine{
		app:           app,
		pendingTopics: map[string]heartbeatPendingTopic{},
	}
	seed := HeartbeatTask{
		ID:           "model-task",
		Title:        "Model task",
		Prompt:       "ping",
		ApprovalMode: "auto",
		Model:        "nope/missing",
	}
	if err := engine.saveTasks([]HeartbeatTask{seed}); err != nil {
		t.Fatal(err)
	}
	engine.ReloadConfig()
	engine.applyTaskModel = func(tabID, model string) error {
		return errors.New("unknown model \"nope/missing\"")
	}
	ctrl := &heartbeatExecuteTaskCtrlStub{}
	injectHeartbeatStubCtrl(app, ctrl)

	got := engine.executeTask(seed)

	if len(ctrl.submitted) != 0 {
		t.Fatalf("submitted prompts = %v, want none when model switch fails", ctrl.submitted)
	}
	if got.LastRunAt == 0 {
		t.Fatal("failed model switch should advance LastRunAt so the next tick retries")
	}
}
