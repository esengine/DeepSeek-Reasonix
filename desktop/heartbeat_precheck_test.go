package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatPrecheckSkipsTaskWhenGateSkips(t *testing.T) {
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
		ID:                     "gated",
		Title:                  "Gated",
		Prompt:                 "ping",
		Precheck:               "exit 2",
		NewConversationEachRun: true,
		ApprovalMode:           "auto",
	}
	got := engine.executeTask(seed)
	if got.TopicID != "" {
		t.Fatalf("skipped task should not create a topic, got %q", got.TopicID)
	}
	if len(got.RunHistory) != 0 {
		t.Fatalf("skipped task should not record a run, got %v", got.RunHistory)
	}
	if got.LastRunAt == 0 {
		t.Fatal("skipped task should advance LastRunAt so it is re-evaluated next interval")
	}
	if got.LastSkippedAt == 0 {
		t.Fatal("skipped task should record LastSkippedAt")
	}
	if len(got.PrecheckHistory) != 1 || got.PrecheckHistory[0].Status != "skipped" || got.PrecheckHistory[0].Summary == "" {
		t.Fatalf("skipped task should record one skipped outcome, got %+v", got.PrecheckHistory)
	}
	if strings.HasPrefix(got.LastSkippedReason, "precheck failed:") {
		t.Fatalf("business skip must not be marked as a failure, got %q", got.LastSkippedReason)
	}
	if len(engine.pendingTopics) != 0 {
		t.Fatalf("skipped task should not leave a pending topic, got %v", engine.pendingTopics)
	}
}

func TestHeartbeatPrecheckFailMarkedDistinctly(t *testing.T) {
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
		ID:       "gated-fail",
		Title:    "Gated Fail",
		Prompt:   "ping",
		Precheck: "exit 1",
	}
	got := engine.executeTask(seed)
	if len(got.PrecheckHistory) != 1 || got.PrecheckHistory[0].Status != "failed" {
		t.Fatalf("failing gate should record a failed outcome, got %+v", got.PrecheckHistory)
	}
	if !strings.HasPrefix(got.LastSkippedReason, "precheck failed:") {
		t.Fatalf("broken gate must be marked as a failure, got %q", got.LastSkippedReason)
	}
	if got.TopicID != "" {
		t.Fatalf("failing gate must not run the task, got topic %q", got.TopicID)
	}
}

func TestHeartbeatPrecheckHistoryCapsRecentOutcomes(t *testing.T) {
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
		ID:              "gated-caps",
		Title:           "Gated Caps",
		Prompt:          "ping",
		Precheck:        "exit 1",
		PrecheckHistory: make([]HeartbeatPrecheckRun, maxPrecheckHistory), // fill to the cap
	}
	got := engine.executeTask(seed)
	if len(got.PrecheckHistory) != maxPrecheckHistory {
		t.Fatalf("precheck history should stay capped at %d, got %d", maxPrecheckHistory, len(got.PrecheckHistory))
	}
	last := got.PrecheckHistory[len(got.PrecheckHistory)-1]
	if last.Status != "failed" || last.At == 0 {
		t.Fatalf("newest outcome should be the failed run just executed, got %+v", last)
	}
}

func TestHeartbeatPrecheckGatePassesAndReceivesPayloadAndCwd(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.runtimeEvents.emit = func(context.Context, string, ...any) {}
	engine := &HeartbeatEngine{
		app:           app,
		pendingTopics: map[string]heartbeatPendingTopic{},
	}
	projectDir := t.TempDir()
	// The gate asserts both the payload on stdin and the working directory,
	// then passes so the task runs its normal path.
	precheck := `grep -q '"event":"HeartbeatPrecheck"' && test "$(pwd)" = "` + projectDir + `"`
	seed := HeartbeatTask{
		ID:                     "passed",
		Title:                  "Passed",
		Prompt:                 "ping",
		Precheck:               precheck,
		Scope:                  "project",
		WorkspaceRoot:          projectDir,
		NewConversationEachRun: true,
		ApprovalMode:           "auto",
	}
	ctrl := &heartbeatExecuteTaskCtrlStub{}
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

	got := engine.executeTask(seed)
	if got.LastSkippedAt != 0 {
		t.Fatalf("passing gate should not skip, LastSkippedAt=%d reason=%q", got.LastSkippedAt, got.LastSkippedReason)
	}
	if got.TopicID == "" {
		t.Fatal("passing gate should proceed to create a topic")
	}
	if len(ctrl.submitted) != 1 || ctrl.submitted[0] != "ping" {
		t.Fatalf("submitted prompts = %v, want [ping]", ctrl.submitted)
	}
	if len(got.PrecheckHistory) != 1 || got.PrecheckHistory[0].Status != "passed" {
		t.Fatalf("passing gate should record one passed outcome, got %+v", got.PrecheckHistory)
	}
}

func TestHeartbeatTruncateReasonCapsLength(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := truncateHeartbeatReason(long)
	if len(got) != 400+len("…") {
		t.Fatalf("truncated length = %d, want %d", len(got), 400+len("…"))
	}
	if short := truncateHeartbeatReason("ok"); short != "ok" {
		t.Fatalf("short reason mutated: %q", short)
	}
}
